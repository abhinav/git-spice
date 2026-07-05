package forgetest

import "crypto/rand"

func randomString(n int) string {
	const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		var buf [1]byte
		_, _ = rand.Read(buf[:])
		idx := int(buf[0]) % len(alnum)
		b[i] = alnum[idx]
	}
	return string(b)
}
