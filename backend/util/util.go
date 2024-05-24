package util

import (
	"crypto/md5"

	"github.com/apex/log"
)

type TeamColor int

const (
	Unknown = 0
	Blue    = 1
	Green   = 2
)

func UserIdToTeam(id string) TeamColor {
	if id == "" {
		log.Errorf("Empty user ID %q, this must not happen.", id)
		return 1
	}
	md5 := md5.Sum([]byte(id))
	return TeamColor(md5[len(md5)-1]%2 + 1)
}
