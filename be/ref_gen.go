package be

import (
	"math/rand"
	"time"
)

var r *rand.Rand

func init() {
	r = rand.New(rand.NewSource(time.Now().UnixNano()))
}

const allowedChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const refLen = 10

func randRefGen() string {
	b := make([]byte, refLen)
	for i := range b {
		b[i] = allowedChars[r.Intn(len(allowedChars))]
	}
	return string(b)
}
