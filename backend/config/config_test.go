package config

import (
	"strconv"
	"sync"
	"testing"
)

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

func TestActiveUserServiceConcurrentRefresh(t *testing.T) {
	configs := &ConfigsData{}
	configs.setActiveUser(&Dedao{User: User{UIDHazy: "initial"}})
	var wg sync.WaitGroup
	for index := 0; index < 50; index++ {
		wg.Add(2)
		go func(index int) {
			defer wg.Done()
			configs.setActiveUser(&Dedao{User: User{UIDHazy: "user-" + strconv.Itoa(index)}})
		}(index)
		go func() {
			defer wg.Done()
			_ = configs.ActiveUserService()
		}()
	}
	wg.Wait()
}
