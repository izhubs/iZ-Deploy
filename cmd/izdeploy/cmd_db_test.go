package main

import (
	"strings"
	"testing"
)

func TestGenerateRandomPassword(t *testing.T) {
	pwd1, err := GenerateRandomPassword(24)
	if err != nil {
		t.Fatalf("unexpected error generating password: %v", err)
	}
	if len(pwd1) != 24 {
		t.Errorf("expected password length 24, got %d", len(pwd1))
	}

	pwd2, err := GenerateRandomPassword(24)
	if err != nil {
		t.Fatalf("unexpected error generating second password: %v", err)
	}
	if pwd1 == pwd2 {
		t.Error("expected different passwords across calls, got identical values")
	}

	// Verify all characters are in alphanumeric charset
	for _, ch := range pwd1 {
		if !strings.ContainsRune(charsetAlpha, ch) {
			t.Errorf("character %q not in allowed charset", ch)
		}
	}
}

func TestBuildDBRunArgs_MariaDB(t *testing.T) {
	engine := supportedEngines["mariadb"]
	name := "wp"
	password := "SecretPass123"
	dbName := "wordpress"
	user := "wp_user"
	port := 3306
	ramMB := 96
	cpu := 0.5
	volume := "wp-data"

	args, dsn, envSnippet := BuildDBRunArgs(engine, name, password, dbName, user, port, ramMB, cpu, volume)

	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "--name izd-db-wp") {
		t.Errorf("expected container name 'izd-db-wp', got: %s", argStr)
	}
	if !strings.Contains(argStr, "-p 127.0.0.1:3306:3306") {
		t.Errorf("expected host port binding 127.0.0.1:3306:3306, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--memory 96m") {
		t.Errorf("expected memory limit 96m, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--innodb_buffer_pool_size=48M") {
		t.Errorf("expected low-RAM innodb buffer pool flag, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--label izdeploy.managed=true") {
		t.Errorf("expected label izdeploy.managed=true, got: %s", argStr)
	}

	expectedDSN := "mysql://wp_user:SecretPass123@127.0.0.1:3306/wordpress?charset=utf8mb4"
	if dsn != expectedDSN {
		t.Errorf("expected DSN %q, got %q", expectedDSN, dsn)
	}

	if !strings.Contains(envSnippet, "DB_CONNECTION=mysql") || !strings.Contains(envSnippet, "DB_PASSWORD=SecretPass123") {
		t.Errorf("unexpected env snippet: %s", envSnippet)
	}
}

func TestBuildDBRunArgs_Postgres(t *testing.T) {
	engine := supportedEngines["postgres"]
	name := "pgapp"
	password := "PgSecret456"
	dbName := "mydb"
	user := "pguser"
	port := 5432
	ramMB := 128
	cpu := 0.5
	volume := ""

	args, dsn, envSnippet := BuildDBRunArgs(engine, name, password, dbName, user, port, ramMB, cpu, volume)

	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "--name izd-db-pgapp") {
		t.Errorf("expected container name 'izd-db-pgapp', got: %s", argStr)
	}
	if !strings.Contains(argStr, "-p 127.0.0.1:5432:5432") {
		t.Errorf("expected host port binding 127.0.0.1:5432:5432, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--memory 128m") {
		t.Errorf("expected memory limit 128m, got: %s", argStr)
	}
	if !strings.Contains(argStr, "-c shared_buffers=32MB") {
		t.Errorf("expected shared_buffers=32MB, got: %s", argStr)
	}

	expectedDSN := "postgres://pguser:PgSecret456@127.0.0.1:5432/mydb?sslmode=disable"
	if dsn != expectedDSN {
		t.Errorf("expected DSN %q, got %q", expectedDSN, dsn)
	}

	if !strings.Contains(envSnippet, "DB_CONNECTION=pgsql") {
		t.Errorf("unexpected env snippet: %s", envSnippet)
	}
}

func TestBuildDBRunArgs_Redis(t *testing.T) {
	engine := supportedEngines["redis"]
	name := "cache"
	password := "RedisAuth789"
	dbName := "0"
	user := "default"
	port := 6379
	ramMB := 32
	cpu := 0.2
	volume := "cache-vol"

	args, dsn, envSnippet := BuildDBRunArgs(engine, name, password, dbName, user, port, ramMB, cpu, volume)

	argStr := strings.Join(args, " ")

	if !strings.Contains(argStr, "--name izd-db-cache") {
		t.Errorf("expected container name 'izd-db-cache', got: %s", argStr)
	}
	if !strings.Contains(argStr, "--memory 32m") {
		t.Errorf("expected memory limit 32m, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--maxmemory 24mb") || !strings.Contains(argStr, "--maxmemory-policy allkeys-lru") {
		t.Errorf("expected low-RAM redis flags, got: %s", argStr)
	}

	expectedDSN := "redis://:RedisAuth789@127.0.0.1:6379/0"
	if dsn != expectedDSN {
		t.Errorf("expected DSN %q, got %q", expectedDSN, dsn)
	}

	if !strings.Contains(envSnippet, "REDIS_HOST=127.0.0.1") {
		t.Errorf("unexpected env snippet: %s", envSnippet)
	}
}

func TestNewDBCmd_Structure(t *testing.T) {
	cmd := newDBCmd()
	if cmd.Use != "db" {
		t.Errorf("expected Use 'db', got %q", cmd.Use)
	}

	expectedSubcommands := map[string]bool{
		"create": false,
		"list":   false,
		"stop":   false,
		"start":  false,
		"remove": false,
	}

	for _, sub := range cmd.Commands() {
		name := strings.Split(sub.Use, " ")[0]
		if _, ok := expectedSubcommands[name]; ok {
			expectedSubcommands[name] = true
		}
	}

	for name, found := range expectedSubcommands {
		if !found {
			t.Errorf("expected subcommand %q not found under 'db'", name)
		}
	}
}
