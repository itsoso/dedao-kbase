package config

import "testing"

func TestSetActiveUserRefreshesService(t *testing.T) {
	configs := &ConfigsData{}
	configs.setActiveUser(&Dedao{User: User{UIDHazy: "first"}})
	firstService := configs.ActiveUserService()

	configs.setActiveUser(&Dedao{User: User{UIDHazy: "second"}})
	secondService := configs.ActiveUserService()
	if firstService == secondService {
		t.Fatal("active user change reused the previous user's service")
	}
}
