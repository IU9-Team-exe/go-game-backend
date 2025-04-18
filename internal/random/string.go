package random

import (
	"math/rand"
	"strings"
	"time"
)

var randSrc rand.Source

func init() {
	randSrc = rand.NewSource(time.Now().UnixNano())
}

func RandString(n int) string {
	letterBytes := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterIdxBits := 6                    // 6 bits to represent a letter index
	letterIdxMask := 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax := 63 / letterIdxBits    // # of letter indices fitting in 63 bits
	sb := strings.Builder{}
	sb.Grow(n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, randSrc.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = randSrc.Int63(), letterIdxMax
		}
		if idx := int(cache & int64(letterIdxMask)); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}
