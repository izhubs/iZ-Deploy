package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

// DECISION: Provide simple CLI command to backup volumes via tar or s3.
// WHY: Prevent data loss for SQLite/stateful apps on resource-constrained VPS.
// TRADE-OFF: Executes shell commands rather than using native Docker API to save binary size.
// REF: wiki/projects/izdeploy/izdeploy_roadmap_v0.0.2.md

func newVolumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage persistent application volumes",
	}

	var s3Target string

	backupCmd := &cobra.Command{
		Use:   "backup [NAME]",
		Short: "Backup an application volume to a local tar archive or S3",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			volumeName := args[0]

			timestamp := time.Now().Format("20060102_150405")
			tarFile := fmt.Sprintf("%s_backup_%s.tar.gz", volumeName, timestamp)

			cmd.Printf("Backing up volume %s...\n", volumeName)

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}

			tarCmd := exec.Command("docker", "run", "--rm",
				"-v", fmt.Sprintf("%s:/data", volumeName),
				"-v", fmt.Sprintf("%s:/backup", cwd),
				"alpine", "tar", "czf", fmt.Sprintf("/backup/%s", tarFile), "-C", "/data", ".")

			tarCmd.Stdout = os.Stdout
			tarCmd.Stderr = os.Stderr

			if err := tarCmd.Run(); err != nil {
				return fmt.Errorf("failed to create backup archive: %w", err)
			}

			cmd.Printf("Volume backed up successfully to %s\n", tarFile)

			if s3Target != "" {
				cmd.Printf("Uploading %s to S3 (%s)...\n", tarFile, s3Target)
				
				s3Cmd := exec.Command("aws", "s3", "cp", tarFile, s3Target)
				s3Cmd.Stdout = os.Stdout
				s3Cmd.Stderr = os.Stderr

				if err := s3Cmd.Run(); err != nil {
					return fmt.Errorf("failed to upload to S3: %w", err)
				}

				cmd.Printf("Backup uploaded to S3 successfully.\n")
			}

			return nil
		},
	}

	backupCmd.Flags().StringVar(&s3Target, "s3", "", "Upload backup to an S3 bucket URL (e.g. s3://mybucket/backups/)")
	cmd.AddCommand(backupCmd)

	return cmd
}
