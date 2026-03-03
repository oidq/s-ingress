package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	. "github.com/onsi/gomega"
)

func getPodLogs(client *kubernetes.Clientset, name, namespace string, opts *v1.PodLogOptions) (string, error) {
	req := client.CoreV1().Pods(namespace).GetLogs(name, opts)
	podLogs, err := req.Stream(context.Background())
	if err != nil {
		return "", fmt.Errorf("error getting pod logs: %s", err)
	}
	defer podLogs.Close()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", fmt.Errorf("error copying log logs: %s", err)
	}
	return buf.String(), nil
}

func findMatchingLines(lines, substring string) []string {
	var matches []string
	for _, line := range strings.Split(lines, "\n") {
		if strings.Contains(line, substring) {
			matches = append(matches, line)
		}
	}

	return matches
}

func getIngressLogLine(client *kubernetes.Clientset, name string, substring string) ([]string, error) {
	ctx := context.Background()
	req, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", name),
	})
	if err != nil {
		return nil, fmt.Errorf("pod logs: %s", err)
	}

	var lines []string
	for _, pod := range req.Items {
		logs, err := getPodLogs(client, pod.Name, pod.Namespace, &v1.PodLogOptions{
			Container: "s-ingress",
		})
		if err != nil {
			return nil, fmt.Errorf("pod %s logs: %s", pod.Name, err)
		}

		matches := findMatchingLines(logs, substring)
		lines = append(lines, matches...)
	}

	return lines, nil
}

func GetIngressLogLine(client *kubernetes.Clientset, name string, substring string) []string {
	lines, err := getIngressLogLine(client, name, substring)
	Expect(err).ToNot(HaveOccurred())

	return lines
}
