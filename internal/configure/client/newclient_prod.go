//go:build !unit

package client

import "github.com/caffeaun/marc/internal/minioclient"

// defaultNewClient returns the production MinIO client constructor.
// This function is excluded from unit builds because minioclient.New
// depends on the minio-go assembly that is also excluded under the unit tag.
func defaultNewClient() func(minioclient.Config) (minioclient.Client, error) {
	return minioclient.New
}
