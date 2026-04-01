// Package client provides server discovery on the local network.
package client

import (
	"fmt"
	"net"
	"sync"
	"time"
)

const defaultPort = 9473

// DiscoverServers scans a /24 subnet for notbbg servers on the default TCP port.
// subnet should be like "192.168.1" (the first three octets).
func DiscoverServers(subnet string, timeout time.Duration) []string {
	if timeout == 0 {
		timeout = 200 * time.Millisecond
	}

	var (
		mu    sync.Mutex
		found []string
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 32) // concurrency limit
	)

	for i := 1; i < 255; i++ {
		addr := fmt.Sprintf("%s.%d:%d", subnet, i, defaultPort)
		wg.Add(1)
		sem <- struct{}{}
		go func(addr string) {
			defer wg.Done()
			defer func() { <-sem }()

			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err != nil {
				return
			}
			conn.Close()

			mu.Lock()
			found = append(found, addr)
			mu.Unlock()
		}(addr)
	}

	wg.Wait()
	return found
}
