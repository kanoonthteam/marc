//go:build unit

package client

import "github.com/caffeaun/marc/internal/minioclient"

// defaultNewClient returns nil under the unit build tag.
// Tests must always supply Options.NewClient explicitly.
func defaultNewClient() func(minioclient.Config) (minioclient.Client, error) {
	return nil
}
