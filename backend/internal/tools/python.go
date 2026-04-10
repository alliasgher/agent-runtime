package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func NewRunPythonTool() *Tool {
	return &Tool{
		Name:        "run_python",
		Description: "Execute Python code and return the output. Use this for calculations, data processing, or generating results. The code should print its output to stdout.",
		Parameters: map[string]ParameterDef{
			"code": {Type: "string", Description: "Python code to execute", Required: true},
		},
		Execute: executePython,
	}
}

func executePython(ctx context.Context, args map[string]any) (string, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return "", fmt.Errorf("code is required")
	}

	// Find python executable
	pythonBin := findPython()
	if pythonBin == "" {
		return "", fmt.Errorf("python3 not found on system")
	}

	// Write code to temp file
	tmpDir, err := os.MkdirTemp("", "agent-python-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "script.py")
	if err := os.WriteFile(scriptPath, []byte(code), 0644); err != nil {
		return "", fmt.Errorf("failed to write script: %w", err)
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, pythonBin, scriptPath)
	cmd.Dir = tmpDir

	// Capture both stdout and stderr
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	output := stdout.String()
	errOutput := stderr.String()

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("execution timed out after 30 seconds")
		}
		// Return stderr as part of the result so the agent can see the error
		return fmt.Sprintf("Error:\n%s\n%s", errOutput, output), nil
	}

	if errOutput != "" && output == "" {
		return errOutput, nil
	}

	return output, nil
}

func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
