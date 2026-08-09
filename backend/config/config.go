package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"github.com/yann0917/dedao-gui/backend/services"
)

const (
	// EnvConfigDir 配置路径环境变量
	EnvConfigDir = "DEDAO_GO_CONFIG_DIR"
	// Name 配置文件名
	Name = "config.json"
)

var (
	configFilePath = filepath.Join(GetConfigDir(), Name)

	// Instance 配置信息 全局调用
	Instance = newLazyConfigsData(configFilePath)
)

// DedaoUsers user
type DedaoUsers []*Dedao

// ConfigsData Configs data
type ConfigsData struct {
	AcitveUID      string
	DownloadPath   string
	Users          DedaoUsers
	activeUser     *Dedao
	configFilePath string
	configFile     *os.File
	fileMu         sync.Mutex
	accountMu      sync.RWMutex
	service        *services.Service
	lazyInit       bool
	initOnce       sync.Once
	initErr        error
}

type configJSONExport struct {
	AcitveUID string
	Users     DedaoUsers
}

func newLazyConfigsData(configFilePath string) *ConfigsData {
	return &ConfigsData{configFilePath: configFilePath, lazyInit: true}
}

func (c *ConfigsData) ensureInitialized() error {
	if c == nil {
		return errors.New("配置未初始化")
	}
	if !c.lazyInit {
		return nil
	}
	c.initOnce.Do(func() {
		c.initErr = c.initialize()
	})
	return c.initErr
}

func (c *ConfigsData) initialize() error {
	if c.configFilePath == "" {
		return errors.New("配置文件未找到")
	}

	// 从配置文件中加载配置
	err := c.loadConfigFromFile()
	if err != nil {
		return err
	}

	// 初始化登陆用户信息
	err = c.initActiveUser()
	if err != nil {
		return nil
	}

	if c.activeUser != nil {
		c.service = c.activeUser.New()
	}

	return nil
}

func (c *ConfigsData) initActiveUser() error {
	// 如果已经初始化过，则跳过
	if c.AcitveUID != "" && c.activeUser != nil && c.activeUser.UIDHazy == c.AcitveUID {
		return nil
	}

	if c.AcitveUID == "" && c.activeUser != nil {
		c.AcitveUID = c.activeUser.UIDHazy
		return nil
	}

	if c.AcitveUID != "" {
		for _, user := range c.Users {
			if user.UIDHazy == c.AcitveUID {
				c.activeUser = user
				return nil
			}
		}
	}

	if c.AcitveUID == "" && len(c.Users) == 0 {
		c.activeUser = new(Dedao)
	}

	if len(c.Users) > 0 {
		return errors.New("存在登录的用户，可以进行切换登录用户")
	}

	return errors.New("未登陆")
}

// Save 保存配置
func (c *ConfigsData) Save() error {
	if err := c.ensureInitialized(); err != nil {
		return err
	}
	return c.saveConfig()
}

func (c *ConfigsData) saveConfig() error {
	if err := c.lazyOpenConfigFile(); err != nil {
		return err
	}
	c.accountMu.RLock()
	activeUID := c.AcitveUID
	users := append(DedaoUsers(nil), c.Users...)
	c.accountMu.RUnlock()

	c.fileMu.Lock()
	defer c.fileMu.Unlock()

	// 保存配置的数据
	conf := configJSONExport{
		AcitveUID: activeUID,
		Users:     users,
	}

	data, err := jsoniter.MarshalIndent(conf, "", " ")

	if err != nil {
		return err
	}

	// 减掉多余的部分
	err = c.configFile.Truncate(int64(len(data)))
	if err != nil {
		// fmt.Println(err)
		return err
	}

	_, err = c.configFile.Seek(0, io.SeekStart)
	if err != nil {
		// fmt.Println(err)
		return err
	}

	_, err = c.configFile.Write(data)
	if err != nil {
		// fmt.Println(err)
		return err
	}

	return nil
}

func (c *ConfigsData) loadConfigFromFile() error {
	err := c.lazyOpenConfigFile()
	if err != nil {
		return err
	}

	info, err := c.configFile.Stat()
	if err != nil {
		return err
	}

	if info.Size() == 0 {
		return c.saveConfig()
	}

	c.fileMu.Lock()
	defer c.fileMu.Unlock()

	_, err = c.configFile.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	// 从配置文件中加载配置
	decoder := jsoniter.NewDecoder(c.configFile)
	var conf configJSONExport
	if err := decoder.Decode(&conf); err != nil {
		return err
	}

	c.AcitveUID = conf.AcitveUID
	c.Users = conf.Users
	return nil
}

func (c *ConfigsData) lazyOpenConfigFile() (err error) {
	c.fileMu.Lock()
	defer c.fileMu.Unlock()
	if c.configFile != nil {
		return nil
	}
	if strings.TrimSpace(c.configFilePath) == "" {
		return errors.New("配置文件未找到")
	}
	if err := os.MkdirAll(filepath.Dir(c.configFilePath), 0o700); err != nil {
		return err
	}
	c.configFile, err = os.OpenFile(c.configFilePath, os.O_CREATE|os.O_RDWR, 0o600)
	return err
}

func (c *ConfigsData) DeleteConfigFile() (err error) {
	if c.configFilePath == "" {
		return nil
	}
	err = os.Remove(c.configFilePath)
	if os.IsNotExist(err) {
		return nil
	}
	return
}

// New config
func New(configFilePath string) *ConfigsData {
	return newLazyConfigsData(configFilePath)
}

// GetConfigDir config file dir
func GetConfigDir() string {
	configDir, ok := os.LookupEnv(EnvConfigDir)
	if ok {
		if filepath.IsAbs(configDir) {
			return configDir
		}
	}
	home, ok := os.LookupEnv("HOME")
	if ok {
		return filepath.Join(home, ".config", "dedao")
	}

	return filepath.Join("/tmp", "dedao")
}

// ActiveUserService user
func (c *ConfigsData) ActiveUserService() *services.Service {
	_ = c.ensureInitialized()
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	if c.service == nil {
		if c.activeUser == nil {
			c.activeUser = new(Dedao)
		}
		c.service = c.activeUser.New()
	}
	return c.service
}

// SetUser set user
func (c *ConfigsData) SetUser(u *Dedao) (*Dedao, *services.User, error) {
	if err := c.ensureInitialized(); err != nil {
		return nil, nil, err
	}
	ser := services.NewService(&u.CookieOptions)
	user, err := ser.User()
	if err != nil {
		return nil, nil, err
	}

	dedao := &Dedao{
		User: User{
			UIDHazy: user.UIDHazy,
			Name:    user.Nickname,
			Avatar:  user.Avatar,
		},
		CookieOptions: u.CookieOptions,
	}
	c.accountMu.Lock()
	c.deleteUserLocked(&User{UIDHazy: user.UIDHazy})
	c.Users = append(c.Users, dedao)
	c.setActiveUserLocked(dedao)
	c.accountMu.Unlock()
	return dedao, user, nil
}

// DeleteUser delete
func (c *ConfigsData) DeleteUser(u *User) {
	_ = c.ensureInitialized()
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	c.deleteUserLocked(u)
}

func (c *ConfigsData) deleteUserLocked(u *User) {
	for k, user := range c.Users {
		if user.UIDHazy == u.UIDHazy {
			c.Users = append(c.Users[:k], c.Users[k+1:]...)
			break
		}
	}
}

// ActiveUser active user
func (c *ConfigsData) ActiveUser() *Dedao {
	_ = c.ensureInitialized()
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	if c.activeUser == nil {
		c.activeUser = new(Dedao)
	}
	return c.activeUser
}

func (c *ConfigsData) setActiveUser(u *Dedao) {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	c.setActiveUserLocked(u)
}

func (c *ConfigsData) setActiveUserLocked(u *Dedao) {
	c.AcitveUID = u.UIDHazy
	c.activeUser = u
	c.service = nil
}

// LoginUserCount 登录用户数量
func (c *ConfigsData) LoginUserCount() int {
	_ = c.ensureInitialized()
	c.accountMu.RLock()
	defer c.accountMu.RUnlock()
	return len(c.Users)
}

// SwitchUser switch user
func (c *ConfigsData) SwitchUser(u *User) error {
	if err := c.ensureInitialized(); err != nil {
		return err
	}
	c.accountMu.Lock()
	for _, user := range c.Users {
		if user.UIDHazy == u.UIDHazy {
			c.setActiveUserLocked(user)
			c.accountMu.Unlock()
			return c.Save()
		}
	}
	c.accountMu.Unlock()
	return errors.New("用户不存在")
}
