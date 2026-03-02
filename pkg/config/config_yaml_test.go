package config

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed config.yaml
var config []byte

func TestDefaultConfig_valid(t *testing.T) {
	_, err := ParseYamlConfigWithDefault(nil)
	require.NoError(t, err)
}

var testConfig_mergeData = `
tls:
  defaultTlsSecret: "test"
`

func TestConfig_merge(t *testing.T) {
	conf, err := ParseYamlConfigWithDefault([]byte(testConfig_mergeData))
	require.NoError(t, err)

	require.Equal(t, int64(4096), conf.IngressProxy.MaxBodySize)
	require.Equal(t, "test", conf.Tls.DefaultTlsSecret)
}
