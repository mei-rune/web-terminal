package terminal

import (
	_ "net/http/pprof"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SupportedCiphers xx
var SupportedCiphers = GetSupportedCiphers()
var SupportedKeyExchanges = GetKeyExchanges()

// GetSupportedCiphers xx
func GetSupportedCiphers() []string {
	config := &ssh.ClientConfig{}
	config.SetDefaults()

	for _, cipher := range []string{
		"aes128-cbc",
		"aes128-ctr",
		"aes192-ctr",
		"aes256-ctr",
		"aes128-gcm@openssh.com",
		"chacha20-poly1305@openssh.com",
		"arcfour256",
		"arcfour128",
		"arcfour",
		"3des-cbc",
	} {
		found := false
		for _, defaultCipher := range config.Ciphers {
			if cipher == defaultCipher {
				found = true
				break
			}
		}

		if !found {
			config.Ciphers = append(config.Ciphers, cipher)
		}
	}

	return config.Ciphers
}

func GetKeyExchanges() []string {
	config := &ssh.ClientConfig{}
	config.SetDefaults()

	for _, keyAlg := range []string{
		"diffie-hellman-group1-sha1",
		"diffie-hellman-group14-sha1",
		"ecdh-sha2-nistp256",
		"ecdh-sha2-nistp384",
		"ecdh-sha2-nistp521",
		"curve25519-sha256@libssh.org",
		"diffie-hellman-group-exchange-sha1",
		"diffie-hellman-group-exchange-sha256",
	} {
		found := false
		for _, defaultKeyAlg := range config.KeyExchanges {
			if keyAlg == defaultKeyAlg {
				found = true
				break
			}
		}

		if !found {
			config.KeyExchanges = append(config.KeyExchanges, keyAlg)
		}
	}

	return config.KeyExchanges
}

func init() {
	value := os.Getenv("ssh_key_exchanges")
	// if value == "" {
	//  value = "diffie-hellman-group-exchange-sha256,diffie-hellman-group-exchange-sha1,diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,curve25519-sha256@libssh.org"
	// }
	if value != "" {
		SupportedKeyExchanges = GetKeyExchanges()
		ss := strings.Split(value, ",")
		var newKeyExchanges []string
		for _, s := range ss {
			found := false
			for _, key := range SupportedKeyExchanges {
				if s == key {
					found = true
					break
				}
			}
			if found {
				newKeyExchanges = append(newKeyExchanges, s)
			}
		}
		for _, s := range SupportedKeyExchanges {
			found := false
			for _, key := range newKeyExchanges {
				if s == key {
					found = true
					break
				}
			}
			if !found {
				newKeyExchanges = append(newKeyExchanges, s)
			}
		}

		SupportedKeyExchanges = newKeyExchanges
	}
}
