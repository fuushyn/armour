#!/usr/bin/env node

/**
 * MCP Go Proxy Setup Wizard
 *
 * Interactive CLI wizard for configuring the MCP proxy with:
 * - Auto-detection of existing MCP servers
 * - Policy mode selection (strict, moderate, permissive)
 * - Configuration persistence
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');
const readline = require('readline');

// Colors for terminal output
const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',
  dim: '\x1b[2m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  cyan: '\x1b[36m',
  red: '\x1b[31m',
};

class SetupWizard {
  constructor() {
    this.homeDir = process.env.HOME || process.env.USERPROFILE;
    this.configDir = path.join(this.homeDir, '.claude', 'mcp-proxy');
    this.configPath = path.join(this.configDir, 'servers.json');
    this.policyMode = 'moderate';
    this.detectedServers = [];
    this.selectedServers = [];
  }

  /**
   * Run the complete setup wizard
   */
  async run() {
    console.log(`${colors.bright}${colors.blue}
╔════════════════════════════════════════════════════════════════╗
║              MCP Go Proxy Setup Wizard                         ║
║                                                                ║
║  Configure your security-enhanced MCP proxy in 2 minutes.     ║
╚════════════════════════════════════════════════════════════════╝
${colors.reset}\n`);

    try {
      // Step 1: Create config directory
      await this.ensureConfigDir();

      // Step 2: Detect existing servers
      await this.detectServers();

      // Step 3: Select which servers to proxy
      if (this.detectedServers && this.detectedServers.length > 0) {
        await this.selectServers();
      } else {
        console.log(`${colors.yellow}⚠  No existing MCP servers detected.${colors.reset}`);
        console.log('You can add servers manually later by editing:');
        console.log(`   ${this.configPath}\n`);
      }

      // Step 4: Choose policy mode
      await this.selectPolicyMode();

      // Step 5: Save configuration
      await this.saveConfiguration();

      // Step 6: Display summary
      await this.displaySummary();

      console.log(`${colors.green}✓ Setup complete!${colors.reset}\n`);
      console.log('Your MCP proxy is now ready to use with Claude Code.\n');

      // Return success response for Claude Code integration
      return {
        status: 'success',
        message: `Proxy configured with ${this.selectedServers.length} servers in ${this.policyMode} mode`,
        config_path: this.configPath,
      };
    } catch (error) {
      console.error(`${colors.red}✗ Setup failed: ${error.message}${colors.reset}\n`);
      throw error;
    }
  }

  /**
   * Ensure config directory exists
   */
  async ensureConfigDir() {
    console.log(`${colors.dim}Creating configuration directory...${colors.reset}`);
    try {
      fs.mkdirSync(this.configDir, { recursive: true });
    } catch (error) {
      throw new Error(`Failed to create config directory: ${error.message}`);
    }
  }

  /**
   * Detect existing MCP servers
   */
  async detectServers() {
    console.log(`\n${colors.bright}Step 1: Detecting MCP Servers${colors.reset}`);
    console.log(`${colors.dim}Scanning configuration files...${colors.reset}\n`);

    try {
      // Try to run the Go proxy's detect command
      const detectCmd = path.join(__dirname, '..', 'mcp-proxy');
      let output;

      try {
        output = execSync(`${detectCmd} detect --json 2>/dev/null`, { encoding: 'utf8' });
        this.detectedServers = JSON.parse(output);
      } catch {
        // If Go binary not available, scan manually
        this.detectedServers = await this.manualServerDetection();
      }

      if (!this.detectedServers || this.detectedServers.length === 0) {
        console.log(`${colors.yellow}No MCP servers found in standard locations.${colors.reset}`);
        return;
      }

      console.log(`${colors.green}Found ${this.detectedServers.length} MCP server(s):${colors.reset}\n`);

      this.detectedServers.forEach((server, i) => {
        const transport = server.type || 'unknown';
        console.log(`  ${colors.cyan}${i + 1}.${colors.reset} ${colors.bright}${server.name}${colors.reset} (${transport})`);

        if (server.command) {
          console.log(`     Command: ${colors.dim}${server.command}${colors.reset}`);
        }
        if (server.url) {
          console.log(`     URL: ${colors.dim}${server.url}${colors.reset}`);
        }
        if (server.source) {
          console.log(`     Source: ${colors.dim}${server.source}${colors.reset}`);
        }
        console.log();
      });
    } catch (error) {
      console.warn(`${colors.yellow}Warning: Detection failed, will use manual entry${colors.reset}`);
    }
  }

  /**
   * Manual server detection by reading config files
   */
  async manualServerDetection() {
    const servers = [];
    const configPaths = [
      path.join(this.homeDir, '.claude.json'),  // Claude Code main config
      path.join(this.homeDir, '.claude', '.mcp.json'),
      path.join(this.homeDir, '.claude', 'mcp.json'),
      '.mcp.json',
      path.join(this.homeDir, 'claude_desktop_config.json'),
    ];

    for (const configPath of configPaths) {
      try {
        if (fs.existsSync(configPath)) {
          const content = JSON.parse(fs.readFileSync(configPath, 'utf8'));
          const mcpServers = content.mcpServers || content.servers || {};

          if (typeof mcpServers === 'object' && mcpServers !== null) {
            for (const [name, config] of Object.entries(mcpServers)) {
              // Skip the proxy itself
              if (name === 'mcp-go-proxy' || name.includes('proxy')) {
                continue;
              }

              servers.push({
                name,
                type: config.type || 'unknown',
                command: config.command,
                url: config.url,
                args: config.args,
                source: configPath,
              });
            }
          }
        }
      } catch (error) {
        // Ignore read/parse errors silently
      }
    }

    return servers || [];
  }

  /**
   * Let user select which servers to include
   */
  async selectServers() {
    console.log(`${colors.bright}Step 2: Select Servers${colors.reset}`);
    console.log('Which servers would you like to add to the proxy?\n');

    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
    });

    const question = (prompt) => new Promise(resolve => rl.question(prompt, resolve));

    this.selectedServers = [];

    for (let i = 0; i < this.detectedServers.length; i++) {
      const server = this.detectedServers[i];
      const answer = await question(
        `  Add ${colors.cyan}${server.name}${colors.reset}? ${colors.dim}(y/n)${colors.reset} `
      );

      if (answer.toLowerCase() === 'y' || answer.toLowerCase() === 'yes') {
        this.selectedServers.push(server);
      }
    }

    rl.close();

    if (this.selectedServers.length === 0) {
      console.log(`${colors.yellow}No servers selected. Creating empty configuration.${colors.reset}\n`);
    } else {
      console.log(`\n${colors.green}Selected ${this.selectedServers.length} server(s).${colors.reset}\n`);
    }
  }

  /**
   * Let user select policy mode
   */
  async selectPolicyMode() {
    console.log(`${colors.bright}Step 3: Select Policy Mode${colors.reset}`);
    console.log(`\nChoose your security policy:\n`);

    const policies = {
      1: {
        name: 'strict',
        display: '${colors.red}Strict${colors.reset}',
        desc: 'Maximum security. Blocks sampling, elicitation, destructive tools. Audit all.',
      },
      2: {
        name: 'moderate',
        display: '${colors.yellow}Moderate${colors.reset}',
        desc: 'Balanced. Allows most operations. Blocks obvious destructive tools.',
      },
      3: {
        name: 'permissive',
        display: '${colors.green}Permissive${colors.reset}',
        desc: 'Minimal restrictions. Allow all operations. Minimal auditing.',
      },
    };

    console.log(`  ${colors.red}1.${colors.reset} Strict - ${policies[1].desc}`);
    console.log(`  ${colors.yellow}2.${colors.reset} Moderate - ${policies[2].desc} ${colors.green}(recommended)${colors.reset}`);
    console.log(`  ${colors.green}3.${colors.reset} Permissive - ${policies[3].desc}\n`);

    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
    });

    const answer = await new Promise(resolve => {
      rl.question(`Select (1-3, default 2): `, resolve);
    });

    rl.close();

    const choice = parseInt(answer) || 2;
    this.policyMode = policies[choice]?.name || 'moderate';

    console.log(`${colors.green}✓${colors.reset} Selected ${colors.bright}${this.policyMode}${colors.reset} mode\n`);
  }

  /**
   * Save configuration to file
   */
  async saveConfiguration() {
    console.log(`${colors.dim}Saving configuration...${colors.reset}`);

    const config = {
      servers: this.selectedServers.map(server => ({
        name: server.name,
        transport: server.type === 'http' || server.type === 'sse' ? 'http' : server.type,
        command: server.command,
        args: server.args,
        url: server.url,
      })),
      policy: {
        mode: this.policyMode,
      },
    };

    try {
      fs.writeFileSync(this.configPath, JSON.stringify(config, null, 2));
      console.log(`${colors.green}✓${colors.reset} Configuration saved to ${colors.cyan}${this.configPath}${colors.reset}\n`);
    } catch (error) {
      throw new Error(`Failed to save configuration: ${error.message}`);
    }
  }

  /**
   * Display setup summary
   */
  async displaySummary() {
    console.log(`${colors.bright}Setup Summary${colors.reset}`);
    console.log('═'.repeat(60));
    console.log(`${colors.bright}Configuration:${colors.reset}`);
    console.log(`  Location: ${colors.cyan}${this.configPath}${colors.reset}`);
    console.log(`  Policy: ${colors.bright}${this.policyMode}${colors.reset}`);
    console.log(`  Servers: ${this.selectedServers.length}`);

    if (this.selectedServers.length > 0) {
      console.log(`\n${colors.bright}Proxied Servers:${colors.reset}`);
      this.selectedServers.forEach(server => {
        console.log(`  • ${colors.cyan}${server.name}${colors.reset} (${server.type})`);
      });
    }

    console.log('\n' + '═'.repeat(60));
  }
}

// Run the wizard
const wizard = new SetupWizard();
wizard.run()
  .then(result => {
    // Output result as JSON for Claude Code to parse
    console.log(JSON.stringify(result));
    process.exit(0);
  })
  .catch(error => {
    console.error(JSON.stringify({
      status: 'error',
      message: error.message,
    }));
    process.exit(1);
  });
