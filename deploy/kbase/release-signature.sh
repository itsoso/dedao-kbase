#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  release-signature.sh sign --manifest PATH --signature PATH --signing-key PATH [--openssl-bin PATH]
  release-signature.sh verify --manifest PATH --signature PATH --trusted-public-key PATH [--openssl-bin PATH]
USAGE
}

fail() {
  printf 'release-signature: %s\n' "$*" >&2
  exit 1
}

require_option_value() {
  option="$1"
  value="${2:-}"
  [[ -n "$value" ]] || fail "${option} requires a value"
}

require_executable() {
  value="$1"
  if [[ "$value" == */* ]]; then
    [[ -x "$value" ]] || fail "OpenSSL is not executable: $value"
  else
    command -v "$value" >/dev/null 2>&1 ||
      fail "OpenSSL command not found: $value"
  fi
}

require_regular_file() {
  path="$1"
  label="$2"
  [[ -f "$path" && ! -L "$path" ]] ||
    fail "${label} must be a regular file: $path"
}

file_mode() {
  path="$1"
  if stat -c '%a' "$path" >/dev/null 2>&1; then
    stat -c '%a' "$path"
  else
    stat -f '%Lp' "$path"
  fi
}

require_private_key_mode() {
  path="$1"
  mode="$(file_mode "$path")"
  case "$mode" in
    ""|*[!0-7]*) fail "cannot determine signing key mode" ;;
  esac
  if (( (8#$mode & 077) != 0 )); then
    fail "signing key must not be accessible by group or others"
  fi
}

require_trusted_key_mode() {
  path="$1"
  mode="$(file_mode "$path")"
  case "$mode" in
    ""|*[!0-7]*) fail "cannot determine trusted public key mode" ;;
  esac
  if (( (8#$mode & 022) != 0 )); then
    fail "trusted public key must not be group/other writable"
  fi
}

sign_manifest() {
  manifest=""
  signature=""
  signing_key=""
  openssl_bin="openssl"

  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --manifest)
        require_option_value "$1" "${2:-}"
        manifest="$2"
        shift 2
        ;;
      --signature)
        require_option_value "$1" "${2:-}"
        signature="$2"
        shift 2
        ;;
      --signing-key)
        require_option_value "$1" "${2:-}"
        signing_key="$2"
        shift 2
        ;;
      --openssl-bin)
        require_option_value "$1" "${2:-}"
        openssl_bin="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown sign option: $1"
        ;;
    esac
  done

  [[ -n "$manifest" ]] || fail "sign requires --manifest"
  [[ -n "$signature" ]] || fail "sign requires --signature"
  [[ -n "$signing_key" ]] || fail "sign requires --signing-key"
  require_executable "$openssl_bin"
  require_regular_file "$manifest" "manifest"
  require_regular_file "$signing_key" "signing key"
  require_private_key_mode "$signing_key"
  private_key_text="$(
    "$openssl_bin" pkey \
      -in "$signing_key" \
      -text \
      -noout 2>/dev/null
  )" || fail "signing key is not a valid private key"
  private_key_bits=""
  while IFS= read -r line; do
    if [[ "$line" =~ ^Private-Key:\ \(([0-9]+)\ bit ]]; then
      private_key_bits="${BASH_REMATCH[1]}"
      break
    fi
    if [[ "$line" =~ ^RSA\ Private-Key:\ \(([0-9]+)\ bit ]]; then
      private_key_bits="${BASH_REMATCH[1]}"
      break
    fi
  done <<<"$private_key_text"
  [[ -n "$private_key_bits" ]] ||
    fail "signing key must be RSA"
  ((private_key_bits >= 3072)) ||
    fail "signing RSA key must contain at least 3072 bits"
  [[ ! -e "$signature" && ! -L "$signature" ]] ||
    fail "signature output already exists: $signature"

  signature_parent="${signature%/*}"
  if [[ "$signature_parent" == "$signature" ]]; then
    signature_parent="."
  elif [[ -z "$signature_parent" ]]; then
    signature_parent="/"
  fi
  [[ -d "$signature_parent" ]] ||
    fail "signature output parent does not exist: $signature_parent"

  temporary_signature="$(
    mktemp "${signature_parent}/.MANIFEST.sig.staging.XXXXXX"
  )"
  cleanup_signature() {
    if [[ -n "${temporary_signature:-}" && -e "$temporary_signature" ]]; then
      rm -f "$temporary_signature"
    fi
  }
  trap cleanup_signature EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  "$openssl_bin" dgst \
    -sha256 \
    -sign "$signing_key" \
    -out "$temporary_signature" \
    "$manifest"
  [[ -s "$temporary_signature" ]] ||
    fail "OpenSSL produced an empty signature"
  chmod 0644 "$temporary_signature"
  mv "$temporary_signature" "$signature"
  temporary_signature=""
}

verify_manifest() {
  manifest=""
  signature=""
  trusted_public_key=""
  openssl_bin="openssl"

  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --manifest)
        require_option_value "$1" "${2:-}"
        manifest="$2"
        shift 2
        ;;
      --signature)
        require_option_value "$1" "${2:-}"
        signature="$2"
        shift 2
        ;;
      --trusted-public-key)
        require_option_value "$1" "${2:-}"
        trusted_public_key="$2"
        shift 2
        ;;
      --openssl-bin)
        require_option_value "$1" "${2:-}"
        openssl_bin="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown verify option: $1"
        ;;
    esac
  done

  [[ -n "$manifest" ]] || fail "verify requires --manifest"
  [[ -n "$signature" ]] || fail "verify requires --signature"
  [[ -n "$trusted_public_key" ]] ||
    fail "verify requires --trusted-public-key"
  require_executable "$openssl_bin"
  require_regular_file "$manifest" "manifest"
  require_regular_file "$signature" "signature"
  require_regular_file "$trusted_public_key" "trusted public key"
  require_trusted_key_mode "$trusted_public_key"

  public_key_text="$(
    "$openssl_bin" pkey \
      -pubin \
      -in "$trusted_public_key" \
      -text \
      -noout 2>/dev/null
  )" || fail "trusted public key is not a valid public key"
  public_key_bits=""
  while IFS= read -r line; do
    if [[ "$line" =~ ^Public-Key:\ \(([0-9]+)\ bit\)$ ]]; then
      public_key_bits="${BASH_REMATCH[1]}"
      break
    fi
    if [[ "$line" =~ ^RSA\ Public-Key:\ \(([0-9]+)\ bit\)$ ]]; then
      public_key_bits="${BASH_REMATCH[1]}"
      break
    fi
  done <<<"$public_key_text"
  [[ -n "$public_key_bits" ]] ||
    fail "cannot determine trusted RSA public key size"
  ((public_key_bits >= 3072)) ||
    fail "trusted RSA public key must contain at least 3072 bits"
  read -r signature_bytes _ < <(wc -c "$signature")
  expected_signature_bytes="$(( (public_key_bits + 7) / 8 ))"
  [[ "$signature_bytes" == "$expected_signature_bytes" ]] ||
    fail "RSA signature size does not match trusted public key"

  "$openssl_bin" dgst \
    -sha256 \
    -verify "$trusted_public_key" \
    -signature "$signature" \
    "$manifest" >/dev/null ||
    fail "manifest signature verification failed"
}

main() {
  operation="${1:-}"
  if [[ -z "$operation" ]]; then
    usage >&2
    exit 2
  fi
  shift

  case "$operation" in
    sign) sign_manifest "$@" ;;
    verify) verify_manifest "$@" ;;
    -h|--help|help) usage ;;
    *) fail "unknown operation: $operation" ;;
  esac
}

main "$@"
