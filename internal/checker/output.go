package checker

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Output formats and prints the check result
func Output(result *CheckResult, format string) error {
	switch format {
	case "json":
		return outputJSON(result)
	default:
		return outputText(result)
	}
}

func outputJSON(result *CheckResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func outputText(result *CheckResult) error {
	// Header
	fmt.Println()
	printHeader("MCP Gateway Configuration Check")
	fmt.Println()

	for _, us := range result.Upstreams {
		// Upstream header
		fmt.Printf("📡 Upstream: %s\n", colorCyan(us.Name))
		fmt.Printf("   Endpoint: %s\n", us.Endpoint)
		fmt.Println()

		// Sort MCP servers by name for consistent output
		servers := make([]MCPStatus, len(us.MCPServers))
		copy(servers, us.MCPServers)
		sort.Slice(servers, func(i, j int) bool {
			return servers[i].Name < servers[j].Name
		})

		for _, mcp := range servers {
			printMCPStatus(mcp)
		}
	}

	// Summary
	printSummary(result)

	return nil
}

func printMCPStatus(mcp MCPStatus) {
	// Status icon and color
	var statusIcon, statusText string
	if mcp.Disabled {
		statusIcon = "⏸️"
		statusText = colorYellow("Disabled")
	} else if mcp.Healthy {
		statusIcon = "✅"
		statusText = colorGreen("Connected")
	} else {
		statusIcon = "❌"
		statusText = colorRed("Failed")
	}

	// Server name with type
	fmt.Printf("   %s [%s] (%s)\n", statusIcon, colorBold(mcp.Name), mcp.Type)

	// Connection info
	if mcp.URL != "" {
		fmt.Printf("      URL: %s\n", mcp.URL)
	}
	if mcp.Command != "" {
		args := ""
		if len(mcp.Args) > 0 {
			args = " " + strings.Join(mcp.Args, " ")
		}
		fmt.Printf("      Command: %s%s\n", mcp.Command, args)
	}

	// Status
	fmt.Printf("      Status: %s\n", statusText)

	if mcp.Disabled {
		fmt.Println()
		return
	}

	if mcp.Healthy {
		// Server info
		fmt.Printf("      Server: %s v%s\n", mcp.ServerName, mcp.ServerVer)

		// Tools
		fmt.Printf("      Tools (%d):\n", len(mcp.Tools))
		for _, t := range mcp.Tools {
			desc := t.Description
			if len(desc) > 45 {
				desc = desc[:45] + "..."
			}
			if desc == "" {
				desc = "-"
			}
			fmt.Printf("        • %-28s %s\n", t.Name, colorDim(desc))
		}
	} else {
		// Error message
		fmt.Printf("      Error: %s\n", colorRed(mcp.Error))
	}

	fmt.Println()
}

func printHeader(title string) {
	line := strings.Repeat("=", 50)
	fmt.Println(colorCyan(line))
	padding := (50 - len(title)) / 2
	fmt.Printf("%s%s%s\n", strings.Repeat(" ", padding), colorBold(title), strings.Repeat(" ", padding))
	fmt.Println(colorCyan(line))
}

func printSummary(result *CheckResult) {
	fmt.Println(colorCyan(strings.Repeat("-", 50)))
	fmt.Println(colorBold("📊 Summary"))
	fmt.Println(colorCyan(strings.Repeat("-", 50)))

	fmt.Printf("   Total Upstreams:    %d\n", len(result.Upstreams))
	fmt.Printf("   Total MCP Servers:  %d\n", result.TotalServers)

	// Detailed status
	if result.HealthyCount > 0 {
		fmt.Printf("     • Healthy:        %s\n", colorGreen(fmt.Sprintf("%d", result.HealthyCount)))
	}
	if result.FailedCount > 0 {
		fmt.Printf("     • Failed:         %s\n", colorRed(fmt.Sprintf("%d", result.FailedCount)))
	}
	if result.DisabledCount > 0 {
		fmt.Printf("     • Disabled:       %s\n", colorYellow(fmt.Sprintf("%d", result.DisabledCount)))
	}

	fmt.Printf("   Total Tools:        %d\n", result.TotalTools)
	fmt.Println(colorCyan(strings.Repeat("=", 50)))
	fmt.Println()
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRedFg  = "\033[31m"
	colorGreenFg = "\033[32m"
	colorYellowFg = "\033[33m"
	colorCyanFg = "\033[36m"
	colorBoldFg = "\033[1m"
	colorDimFg  = "\033[2m"
)

// Check if output supports colors
var useColors = true

func init() {
	// Disable colors if not a terminal or NO_COLOR is set
	if os.Getenv("NO_COLOR") != "" {
		useColors = false
	}
	// Simple check for terminal
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		useColors = false
	}
}

func colorRed(s string) string {
	if !useColors {
		return s
	}
	return colorRedFg + s + colorReset
}

func colorGreen(s string) string {
	if !useColors {
		return s
	}
	return colorGreenFg + s + colorReset
}

func colorYellow(s string) string {
	if !useColors {
		return s
	}
	return colorYellowFg + s + colorReset
}

func colorCyan(s string) string {
	if !useColors {
		return s
	}
	return colorCyanFg + s + colorReset
}

func colorBold(s string) string {
	if !useColors {
		return s
	}
	return colorBoldFg + s + colorReset
}

func colorDim(s string) string {
	if !useColors {
		return s
	}
	return colorDimFg + s + colorReset
}
