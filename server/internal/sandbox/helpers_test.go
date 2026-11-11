package sandbox

import "golang.org/x/crypto/ssh"

func (c *hostKeyCache) get(addr string) ssh.PublicKey {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[addr].key
}
