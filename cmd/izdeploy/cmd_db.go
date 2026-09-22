package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// DECISION: Provide 1-click database provisioning tuned for constrained Linux VPS (>=512MB RAM).
// WHY: Applications like iz-wp-lite and microservices require ultra-low memory database engines
// without manual cgroup and buffer tuning, preventing host OOM termination.
// TRADE-OFF: Default buffer pools are restricted (e.g. MariaDB 48M pool, Redis 24M), unsuitable for high-traffic write loads.
// REF: wiki/projects/izdeploy/izdeploy_roadmap_v0.0.2.md

const (
	labelManaged = "izdeploy.managed=true"
	labelDBType  = "izdeploy.db.type="
	labelApp     = "izdeploy.app=db-"
	charsetAlpha = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// DBEngineConfig holds default runtime, image, and buffer configurations for a database engine.
type DBEngineConfig struct {
	Type        string
	Image       string
	DefaultPort int
	DefaultRAM  int
	DefaultCPU  float64
	DefaultDB   string
	DefaultUser string
}

var supportedEngines = map[string]DBEngineConfig{
	"mariadb": {
		Type:        "mariadb",
		Image:       "mariadb:11.4",
		DefaultPort: 3306,
		DefaultRAM:  96,
		DefaultCPU:  0.5,
		DefaultDB:   "app",
		DefaultUser: "app",
	},
	"postgres": {
		Type:        "postgres",
		Image:       "postgres:16-alpine",
		DefaultPort: 5432,
		DefaultRAM:  128,
		DefaultCPU:  0.5,
		DefaultDB:   "app",
		DefaultUser: "app",
	},
	"redis": {
		Type:        "redis",
		Image:       "redis:7-alpine",
		DefaultPort: 6379,
		DefaultRAM:  32,
		DefaultCPU:  0.2,
		DefaultDB:   "0",
		DefaultUser: "default",
	},
}

// GenerateRandomPassword generates a cryptographically secure alphanumeric password of specified length.
//
// Business rule: Enforces high-entropy credentials for newly provisioned database instances.
//
// @ai-constraint: Never use pseudo-random generators (math/rand) for security credentials.
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		length = 24
	}
	result := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charsetAlpha)))

	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("failed generating random credential: %w", err)
		}
		result[i] = charsetAlpha[idx.Int64()]
	}
	return string(result), nil
}

// BuildDBRunArgs constructs the docker run arguments with low-RAM optimizations.
func BuildDBRunArgs(engine DBEngineConfig, name, password, dbName, user string, port, ramMB int, cpu float64, volume string) ([]string, string, string) {
	containerName := fmt.Sprintf("izd-db-%s", name)
	if volume == "" {
		volume = fmt.Sprintf("izd-data-%s", name)
	}

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--restart", "unless-stopped",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, engine.DefaultPort),
		"--memory", fmt.Sprintf("%dm", ramMB),
		"--cpus", fmt.Sprintf("%.2f", cpu),
		"-v", fmt.Sprintf("%s:/var/lib/%s", volume, volumeSubdir(engine.Type)),
		"--label", labelManaged,
		"--label", labelDBType + engine.Type,
		"--label", labelApp + name,
	}

	var dsn string
	var envSnippet string

	switch engine.Type {
	case "mariadb":
		args = append(args,
			"-e", fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", password),
			"-e", fmt.Sprintf("MYSQL_DATABASE=%s", dbName),
			"-e", fmt.Sprintf("MYSQL_USER=%s", user),
			"-e", fmt.Sprintf("MYSQL_PASSWORD=%s", password),
			engine.Image,
			"--innodb_buffer_pool_size=48M",
			"--key_buffer_size=16M",
			"--max_connections=30",
			"--skip-name-resolve",
		)
		dsn = fmt.Sprintf("mysql://%s:%s@127.0.0.1:%d/%s?charset=utf8mb4", user, password, port, dbName)
		envSnippet = fmt.Sprintf("DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=%d\nDB_DATABASE=%s\nDB_USERNAME=%s\nDB_PASSWORD=%s\nDATABASE_URL=%s",
			port, dbName, user, password, dsn)

	case "postgres":
		args = append(args,
			"-e", fmt.Sprintf("POSTGRES_DB=%s", dbName),
			"-e", fmt.Sprintf("POSTGRES_USER=%s", user),
			"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
			engine.Image,
			"-c", "shared_buffers=32MB",
			"-c", "max_connections=30",
		)
		dsn = fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable", user, password, port, dbName)
		envSnippet = fmt.Sprintf("DB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=%d\nDB_DATABASE=%s\nDB_USERNAME=%s\nDB_PASSWORD=%s\nDATABASE_URL=%s",
			port, dbName, user, password, dsn)

	case "redis":
		args = append(args,
			engine.Image,
			"redis-server",
			"--maxmemory", "24mb",
			"--maxmemory-policy", "allkeys-lru",
			"--requirepass", password,
		)
		dsn = fmt.Sprintf("redis://:%s@127.0.0.1:%d/0", password, port)
		envSnippet = fmt.Sprintf("REDIS_HOST=127.0.0.1\nREDIS_PORT=%d\nREDIS_PASSWORD=%s\nREDIS_URL=%s",
			port, password, dsn)
	}

	return args, dsn, envSnippet
}

func volumeSubdir(engineType string) string {
	switch engineType {
	case "mariadb":
		return "mysql"
	case "postgres":
		return "postgresql/data"
	case "redis":
		return "redis"
	default:
		return "data"
	}
}

func newDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Manage lightweight database instances (mariadb, postgres, redis)",
		Long: `Provisions and manages low-RAM database engines directly on the host VPS.
All instances are tuned with restricted buffer pools and cgroup limits to operate reliably on >=512MB RAM nodes.`,
	}

	cmd.AddCommand(newDBCreateCmd())
	cmd.AddCommand(newDBListCmd())
	cmd.AddCommand(newDBStopCmd())
	cmd.AddCommand(newDBStartCmd())
	cmd.AddCommand(newDBRemoveCmd())

	return cmd
}

func newDBCreateCmd() *cobra.Command {
	var (
		name     string
		port     int
		ramMB    int
		cpu      float64
		volume   string
		password string
		database string
		user     string
		dryRun   bool
	)

	createCmd := &cobra.Command{
		Use:   "create [mariadb|postgres|redis]",
		Short: "Provision a low-RAM database container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engineType := strings.ToLower(args[0])
			engine, ok := supportedEngines[engineType]
			if !ok {
				return fmt.Errorf("unsupported database engine %q (supported: mariadb, postgres, redis)", engineType)
			}

			if name == "" {
				name = engineType
			}

			if port <= 0 {
				port = engine.DefaultPort
			}

			if ramMB <= 0 {
				ramMB = engine.DefaultRAM
			}

			if cpu <= 0 {
				cpu = engine.DefaultCPU
			}

			if database == "" {
				database = engine.DefaultDB
			}

			if user == "" {
				user = engine.DefaultUser
			}

			if password == "" {
				var err error
				password, err = GenerateRandomPassword(24)
				if err != nil {
					return err
				}
			}

			runArgs, dsn, envSnippet := BuildDBRunArgs(engine, name, password, database, user, port, ramMB, cpu, volume)
			containerName := fmt.Sprintf("izd-db-%s", name)

			if dryRun {
				cmd.Println("=== DRY RUN MODE: Database Provisioning Plan ===")
				cmd.Printf("Engine:        %s (%s)\n", engine.Type, engine.Image)
				cmd.Printf("Container:     %s\n", containerName)
				cmd.Printf("Host Port:     127.0.0.1:%d\n", port)
				cmd.Printf("RAM Limit:     %d MB\n", ramMB)
				cmd.Printf("CPU Limit:     %.2f cores\n", cpu)
				cmd.Println("\nCommand to execute:")
				cmd.Printf("docker %s\n\n", strings.Join(runArgs, " "))
				cmd.Println("Connection DSN:")
				cmd.Printf("  %s\n\n", dsn)
				cmd.Println("Environment snippet (.env):")
				cmd.Println(envSnippet)
				return nil
			}

			cmd.Printf("Provisioning %s database container '%s' (RAM: %dMB, Port: %d)...\n", engine.Type, containerName, ramMB, port)

			dockerCmd := exec.Command("docker", runArgs...)
			dockerCmd.Stderr = os.Stderr
			output, err := dockerCmd.Output()
			if err != nil {
				return fmt.Errorf("failed executing docker run for %s: %w", containerName, err)
			}

			containerID := strings.TrimSpace(string(output))
			if len(containerID) > 12 {
				containerID = containerID[:12]
			}

			cmd.Printf("\nDatabase container successfully provisioned:\n")
			cmd.Printf("  - Container ID: %s\n", containerID)
			cmd.Printf("  - Container:    %s\n", containerName)
			cmd.Printf("  - Host Binding: 127.0.0.1:%d\n", port)
			cmd.Printf("  - Memory Cap:   %dMB\n\n", ramMB)
			cmd.Println("Connection DSN:")
			cmd.Printf("  %s\n\n", dsn)
			cmd.Println("Environment snippet (.env):")
			cmd.Println(envSnippet)

			return nil
		},
	}

	createCmd.Flags().StringVar(&name, "name", "", "Instance name identifier (defaults to engine type)")
	createCmd.Flags().IntVar(&port, "port", 0, "Host port binding (defaults to standard engine port)")
	createCmd.Flags().IntVar(&ramMB, "ram", 0, "Memory limit in MB (defaults to engine low-RAM baseline)")
	createCmd.Flags().Float64Var(&cpu, "cpu", 0, "CPU core fraction limit")
	createCmd.Flags().StringVar(&volume, "volume", "", "Volume name or host mount path")
	createCmd.Flags().StringVar(&password, "password", "", "Database password (auto-generated if omitted)")
	createCmd.Flags().StringVar(&database, "database", "", "Initial database name")
	createCmd.Flags().StringVar(&user, "user", "", "Database username")
	createCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview docker run command and credentials without execution")

	return createCmd
}

func newDBListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all izDeploy-managed database instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			dockerCmd := exec.Command("docker", "ps", "-a",
				"--filter", fmt.Sprintf("label=%s", labelManaged),
				"--format", "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}")
			dockerCmd.Stdout = os.Stdout
			dockerCmd.Stderr = os.Stderr
			if err := dockerCmd.Run(); err != nil {
				return fmt.Errorf("failed listing database containers: %w", err)
			}
			return nil
		},
	}
}

func newDBStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [NAME]",
		Short: "Stop a managed database instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			containerName := name
			if !strings.HasPrefix(containerName, "izd-db-") {
				containerName = fmt.Sprintf("izd-db-%s", name)
			}
			cmd.Printf("Stopping database container %s...\n", containerName)
			dockerCmd := exec.Command("docker", "stop", containerName)
			dockerCmd.Stdout = os.Stdout
			dockerCmd.Stderr = os.Stderr
			if err := dockerCmd.Run(); err != nil {
				return fmt.Errorf("failed stopping container %s: %w", containerName, err)
			}
			cmd.Printf("Database container %s stopped.\n", containerName)
			return nil
		},
	}
}

func newDBStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [NAME]",
		Short: "Start a stopped managed database instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			containerName := name
			if !strings.HasPrefix(containerName, "izd-db-") {
				containerName = fmt.Sprintf("izd-db-%s", name)
			}
			cmd.Printf("Starting database container %s...\n", containerName)
			dockerCmd := exec.Command("docker", "start", containerName)
			dockerCmd.Stdout = os.Stdout
			dockerCmd.Stderr = os.Stderr
			if err := dockerCmd.Run(); err != nil {
				return fmt.Errorf("failed starting container %s: %w", containerName, err)
			}
			cmd.Printf("Database container %s started.\n", containerName)
			return nil
		},
	}
}

func newDBRemoveCmd() *cobra.Command {
	var removeVolume bool

	cmd := &cobra.Command{
		Use:   "remove [NAME]",
		Short: "Remove a managed database instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			containerName := name
			if !strings.HasPrefix(containerName, "izd-db-") {
				containerName = fmt.Sprintf("izd-db-%s", name)
			}
			cmd.Printf("Removing database container %s...\n", containerName)

			rmArgs := []string{"rm", "-f"}
			if removeVolume {
				rmArgs = append(rmArgs, "-v")
			}
			rmArgs = append(rmArgs, containerName)

			dockerCmd := exec.Command("docker", rmArgs...)
			dockerCmd.Stdout = os.Stdout
			dockerCmd.Stderr = os.Stderr
			if err := dockerCmd.Run(); err != nil {
				return fmt.Errorf("failed removing container %s: %w", containerName, err)
			}
			cmd.Printf("Database container %s removed.\n", containerName)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&removeVolume, "volume", "v", false, "Also remove associated docker volume")
	return cmd
}
