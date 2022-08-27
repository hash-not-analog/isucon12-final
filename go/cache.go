package main

import (
	"github.com/logica0419/helpisu"
)

var userCache = helpisu.NewCache[int64, *User]()

type Getter interface {
	Get(dest interface{}, query string, args ...interface{}) error
}

func getUserByID(getter Getter, id int64) (*User, error) {
	user, ok := userCache.Get(id)
	if !ok {
		user := &User{}
		query := "SELECT * FROM users WHERE id=?"
		if err := getter.Get(user, query, id); err != nil {
			return nil, err
		}

		userCache.Set(id, user)
	}

	return user, nil
}
