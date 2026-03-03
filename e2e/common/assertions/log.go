package assertions

import (
	"encoding/json"
	"fmt"
	"strings"

	"codeberg.org/oidq/s-ingress/e2e/common"
	"github.com/onsi/gomega/types"

	. "github.com/onsi/gomega"
)

func ExpectIngressLog(name string, substring string) Assertion {
	client := common.GetClient()
	lines := common.GetIngressLogLine(client, name, substring)
	Expect(lines).To(HaveLen(1), "Substring matched multiple lines in %q logs", name)

	return Expect(lines[0])
}

type logAttributeMatcher struct {
	key   string
	value any
}

func (l *logAttributeMatcher) Match(actual any) (bool, error) {
	val, err := getLogLineValue(actual, l.key)
	if err != nil {
		return false, err
	}
	return val == l.value, nil
}

func (l *logAttributeMatcher) FailureMessage(actual any) (message string) {
	return fmt.Sprintf("Expected\n===\n%s\n===\nto have attribute %q set to %q", tryFormat(actual), l.key, l.value)
}

func (l *logAttributeMatcher) NegatedFailureMessage(actual any) (message string) {
	return fmt.Sprintf("Expected\n===\n%s\n===\nto not have attribute %q set to %q", tryFormat(actual), l.key, l.value)
}

func HaveLogAttribute(key, value string) types.GomegaMatcher {
	return &logAttributeMatcher{
		key:   key,
		value: value,
	}
}

type logAttributeSetMatcher struct {
	key string
}

func (l *logAttributeSetMatcher) Match(actual any) (bool, error) {
	val, err := getLogLineValue(actual, l.key)
	if err != nil {
		return false, err
	}
	return val != nil, nil
}

func (l *logAttributeSetMatcher) FailureMessage(actual any) (message string) {
	return fmt.Sprintf("Expected\n===\n%s\n===\nto have attribute %q", tryFormat(actual), l.key)
}

func (l *logAttributeSetMatcher) NegatedFailureMessage(actual any) (message string) {
	return fmt.Sprintf("Expected\n===\n%s\n===\nto not have attribute %q", tryFormat(actual), l.key)
}

func HaveLogAttributeSet(key string) types.GomegaMatcher {
	return &logAttributeSetMatcher{
		key: key,
	}
}

func matchMapKey(m any, key string) any {
	if key == "" {
		return m
	}

	assertedM, ok := m.(map[string]any)
	if !ok {
		return nil
	}

	split := strings.SplitN(key, ".", 2)
	if len(split) < 2 {
		split = append(split, "")
	}

	entry := assertedM[split[0]]
	return matchMapKey(entry, split[1])
}

func getLogLineValue(line any, key string) (any, error) {
	s, ok := line.(string)
	if !ok {
		return nil, fmt.Errorf("expected string")
	}

	data := map[string]any{}
	err := json.Unmarshal([]byte(s), &data)
	if err != nil {
		return nil, err
	}

	return matchMapKey(data, key), nil
}

func tryFormat(value any) string {
	s, ok := value.(string)
	if !ok {
		return fmt.Sprintf("%v", value)
	}

	data := map[string]any{}
	err := json.Unmarshal([]byte(s), &data)
	if err != nil {
		return s
	}

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return s
	}

	return string(out)
}
