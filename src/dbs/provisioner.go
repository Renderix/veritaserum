package dbs

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"veritaserum/src/store"
)

func ProvisionMySQL(id, schema, hydrate string) {
	port, err := findFreePort()
	if err != nil {
		store.FailProvisionedDB(id, fmt.Sprintf("find port: %v", err))
		return
	}

	out, err := exec.Command("docker", "run", "-d",
		"-p", fmt.Sprintf("%d:3306", port),
		"-e", "MYSQL_ROOT_PASSWORD=veritaserum",
		"-e", "MYSQL_DATABASE=app",
		"mysql:8",
	).Output()
	if err != nil {
		store.FailProvisionedDB(id, fmt.Sprintf("docker run: %v", err))
		return
	}
	containerID := strings.TrimSpace(string(out))
	store.UpdateProvisionedDB(id, containerID, port)

	if err := waitForMySQL(containerID, 90*time.Second); err != nil {
		exec.Command("docker", "rm", "-f", containerID).Run()
		store.FailProvisionedDB(id, fmt.Sprintf("mysql not ready: %v", err))
		return
	}

	for _, sql := range []string{schema, hydrate} {
		if sql == "" {
			continue
		}
		cmd := exec.Command("docker", "exec", "-i", containerID,
			"mysql", "-uroot", "-pveritaserum", "app")
		cmd.Stdin = strings.NewReader(sql)
		if err := cmd.Run(); err != nil {
			exec.Command("docker", "rm", "-f", containerID).Run()
			store.FailProvisionedDB(id, fmt.Sprintf("apply sql: %v", err))
			return
		}
	}

	jdbcURL := fmt.Sprintf("jdbc:mysql://localhost:%d/app?user=root&password=veritaserum", port)
	store.ReadyProvisionedDB(id, jdbcURL)
}

func waitForMySQL(containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := exec.Command("docker", "exec", containerID,
			"mysqladmin", "ping", "-uroot", "-pveritaserum", "--silent").Run()
		if err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout after %s", timeout)
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}
