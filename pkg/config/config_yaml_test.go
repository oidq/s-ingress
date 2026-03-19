package config

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
)

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

	require.Equal(t, int64(8000), conf.IngressProxy.MaxBodySize)
	require.Equal(t, "test", conf.Tls.DefaultTlsSecret)
}

var parseSizeTestData = []struct {
	input  string
	output int64
}{
	{"10KiB", 10 * 1024},
	{"10K", 10 * 1000},
	{"10MiB", 10 * 1024 * 1024},
	{"10GiB", 10 * 1024 * 1024 * 1024},
	{"14", 14},
	{"0", 0},
	{"-10GiB", -1},
	{"10.2GiB", -1},
}

func TestParseByteSize(t *testing.T) {
	for _, td := range parseSizeTestData {
		t.Run(td.input, func(t *testing.T) {
			size, err := ParseByteSize(td.input)
			if td.output >= 0 {
				require.NoError(t, err)
				require.Equal(t, td.output, size)
			} else {
				require.Error(t, err)
			}
		})
	}
}
