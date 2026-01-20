package dashboard

// getUnifiedDashboardHTML renders the single-page control plane UI.
func getUnifiedDashboardHTML() string {
	return `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Armour Control Plane</title>
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;600;700&family=JetBrains+Mono:wght@400;600&display=swap');

		:root {
			color-scheme: dark;
			--bg: #0b0e14;
			--bg-alt: #0f1420;
			--panel: #121826;
			--panel-strong: #161f2e;
			--stroke: #253046;
			--muted: #94a3b8;
			--text: #e6edf7;
			--accent: #3bd1c9;
			--accent-strong: #1ae3b1;
			--accent-warm: #f5b56b;
			--danger: #ff6b6b;
			--success: #3ddc97;
			--warning: #f2c94c;
			--shadow: 0 12px 40px rgba(5, 10, 18, 0.55);
			--radius: 18px;
			--mono: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
			--sans: "Space Grotesk", "Segoe UI", sans-serif;
		}

		* {
			box-sizing: border-box;
		}

		body {
			margin: 0;
			font-family: var(--sans);
			background: var(--bg);
			color: var(--text);
			min-height: 100vh;
		}

		body::before {
			content: "";
			position: fixed;
			inset: 0;
			background:
				radial-gradient(1200px 600px at 10% 10%, rgba(59, 209, 201, 0.12), transparent 55%),
				radial-gradient(900px 500px at 90% 18%, rgba(245, 181, 107, 0.12), transparent 60%),
				radial-gradient(700px 600px at 70% 90%, rgba(80, 130, 255, 0.08), transparent 60%),
				linear-gradient(180deg, rgba(18, 24, 38, 0.2), rgba(8, 10, 14, 0.6));
			z-index: -1;
		}

		a {
			color: var(--accent);
			text-decoration: none;
		}

		a:hover {
			color: var(--accent-strong);
		}

		button,
		input,
		select,
		textarea {
			font-family: inherit;
			color: inherit;
		}

		.topbar {
			position: sticky;
			top: 0;
			z-index: 20;
			backdrop-filter: blur(14px);
			background: rgba(11, 14, 20, 0.82);
			border-bottom: 1px solid rgba(37, 48, 70, 0.6);
		}

		.topbar-inner {
			max-width: 1200px;
			margin: 0 auto;
			padding: 16px 20px;
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 16px;
		}

		.brand {
			font-size: 18px;
			font-weight: 600;
			letter-spacing: 0.03em;
			text-transform: uppercase;
			color: var(--text);
		}

		.nav {
			display: flex;
			gap: 14px;
			font-size: 14px;
			flex-wrap: wrap;
		}

		.nav a {
			color: var(--muted);
			padding: 6px 8px;
			border-radius: 999px;
			border: 1px solid transparent;
		}

		.nav a:hover {
			color: var(--text);
			border-color: rgba(61, 220, 151, 0.4);
			background: rgba(61, 220, 151, 0.08);
		}

		.status-pill {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			padding: 6px 12px;
			border-radius: 999px;
			background: rgba(61, 220, 151, 0.1);
			color: var(--success);
			font-size: 12px;
			border: 1px solid rgba(61, 220, 151, 0.4);
		}

		.status-dot {
			width: 8px;
			height: 8px;
			border-radius: 50%;
			background: var(--success);
			box-shadow: 0 0 12px rgba(61, 220, 151, 0.8);
		}

		.app {
			max-width: 1200px;
			margin: 0 auto;
			padding: 28px 20px 80px;
			display: flex;
			flex-direction: column;
			gap: 32px;
		}

		.section {
			display: flex;
			flex-direction: column;
			gap: 20px;
		}

		.section-header {
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 16px;
			flex-wrap: wrap;
		}

		.section-title {
			font-size: 20px;
			letter-spacing: 0.01em;
			margin: 0;
		}

		.card {
			background: linear-gradient(140deg, rgba(18, 24, 38, 0.9), rgba(10, 14, 22, 0.9));
			border: 1px solid rgba(37, 48, 70, 0.8);
			border-radius: var(--radius);
			padding: 20px 22px;
			box-shadow: var(--shadow);
		}

		.hero {
			display: grid;
			grid-template-columns: minmax(0, 1.2fr) minmax(0, 0.8fr);
			gap: 20px;
		}

		.hero h1 {
			font-size: 32px;
			margin: 0 0 12px;
		}

		.hero p {
			margin: 0 0 16px;
			color: var(--muted);
			line-height: 1.6;
		}

		.hero-actions {
			display: flex;
			gap: 12px;
			flex-wrap: wrap;
		}

		.btn {
			border: 1px solid var(--stroke);
			border-radius: 999px;
			padding: 10px 18px;
			background: rgba(21, 29, 45, 0.8);
			cursor: pointer;
			font-size: 14px;
			transition: transform 0.2s ease, border-color 0.2s ease, background 0.2s ease;
			min-height: 40px;
		}

		.btn:hover {
			transform: translateY(-1px);
			border-color: rgba(61, 220, 151, 0.5);
		}

		.btn-primary {
			background: linear-gradient(120deg, #3bd1c9, #2fbf94);
			color: #07151c;
			border: none;
			font-weight: 600;
		}

		.btn-primary:hover {
			border-color: transparent;
			transform: translateY(-2px);
		}

		.btn-ghost {
			background: transparent;
		}

		.btn-danger {
			background: rgba(255, 107, 107, 0.15);
			border-color: rgba(255, 107, 107, 0.4);
			color: var(--danger);
		}

		.btn-danger:hover {
			border-color: rgba(255, 107, 107, 0.8);
		}

		.stat-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
			gap: 16px;
		}

		.stat-card {
			padding: 18px;
			display: flex;
			flex-direction: column;
			gap: 8px;
		}

		.stat-label {
			font-size: 12px;
			text-transform: uppercase;
			letter-spacing: 0.12em;
			color: var(--muted);
		}

		.stat-value {
			font-size: 28px;
			font-weight: 600;
		}

		.stat-sub {
			font-size: 12px;
			color: var(--muted);
		}

		.server-list {
			display: grid;
			gap: 12px;
		}

		.server-item {
			display: flex;
			align-items: center;
			justify-content: space-between;
			padding: 14px 16px;
			border-radius: 14px;
			background: rgba(15, 20, 32, 0.7);
			border: 1px solid rgba(37, 48, 70, 0.6);
		}

		.server-item h3 {
			margin: 0 0 4px;
			font-size: 16px;
		}

		.server-item p {
			margin: 0;
			font-size: 13px;
			color: var(--muted);
		}

		.server-toolbar {
			display: flex;
			align-items: flex-start;
			justify-content: space-between;
			gap: 12px;
			flex-wrap: wrap;
			margin-bottom: 10px;
		}

		.server-form {
			margin-top: 4px;
			padding: 14px 16px;
			border-radius: 14px;
			background: rgba(12, 16, 26, 0.7);
			border: 1px dashed rgba(59, 209, 201, 0.45);
		}

		.server-form-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 12px;
		}

		.server-form-actions {
			display: flex;
			align-items: center;
			gap: 10px;
			margin-top: 12px;
			flex-wrap: wrap;
		}

		.small-label {
			font-size: 12px;
			color: var(--muted);
		}

		.hidden {
			display: none !important;
		}

		.badge {
			display: inline-flex;
			align-items: center;
			gap: 6px;
			padding: 4px 10px;
			border-radius: 999px;
			font-size: 12px;
			border: 1px solid transparent;
		}

		.badge-ok {
			background: rgba(61, 220, 151, 0.12);
			border-color: rgba(61, 220, 151, 0.4);
			color: var(--success);
		}

		.rule-controls {
			display: flex;
			gap: 12px;
			flex-wrap: wrap;
		}

		.input {
			background: rgba(12, 16, 26, 0.8);
			border: 1px solid rgba(37, 48, 70, 0.8);
			border-radius: 12px;
			padding: 10px 12px;
			font-size: 14px;
			min-width: 200px;
		}

		.input:focus {
			outline: 2px solid rgba(59, 209, 201, 0.5);
			outline-offset: 2px;
		}

		.rule-list {
			display: grid;
			gap: 14px;
		}

		details.rule-card {
			border: 1px solid rgba(37, 48, 70, 0.8);
			border-radius: 16px;
			background: rgba(13, 18, 30, 0.8);
			padding: 0;
		}

		details.rule-card summary {
			list-style: none;
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 14px;
			padding: 16px 18px;
			cursor: pointer;
		}

		details.rule-card summary::-webkit-details-marker {
			display: none;
		}

		details.rule-card summary::after {
			content: ">";
			font-size: 14px;
			color: var(--muted);
			transform: rotate(90deg);
			transition: transform 0.2s ease;
		}

		details.rule-card[open] summary::after {
			transform: rotate(-90deg);
		}

		.rule-main {
			display: flex;
			flex-direction: column;
			gap: 6px;
			flex: 1;
		}

		.rule-pattern {
			font-family: var(--mono);
			font-size: 14px;
			color: var(--text);
		}

		.rule-desc {
			font-size: 13px;
			color: var(--muted);
		}

		.rule-meta {
			display: flex;
			gap: 8px;
			flex-wrap: wrap;
		}

		.chip {
			display: inline-flex;
			align-items: center;
			gap: 6px;
			padding: 4px 10px;
			border-radius: 999px;
			font-size: 11px;
			border: 1px solid rgba(37, 48, 70, 0.8);
			color: var(--muted);
			text-transform: uppercase;
			letter-spacing: 0.08em;
		}

		.chip-block {
			background: rgba(255, 107, 107, 0.14);
			color: var(--danger);
			border-color: rgba(255, 107, 107, 0.5);
		}

		.chip-allow {
			background: rgba(61, 220, 151, 0.16);
			color: var(--success);
			border-color: rgba(61, 220, 151, 0.5);
		}

		.chip-on {
			background: rgba(61, 220, 151, 0.12);
			color: var(--success);
		}

		.chip-off {
			background: rgba(245, 181, 107, 0.16);
			color: var(--accent-warm);
		}

		.rule-body {
			padding: 0 18px 18px;
			display: grid;
			gap: 14px;
		}

		.rule-columns {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
			gap: 12px;
		}

		.rule-block {
			background: rgba(10, 14, 22, 0.75);
			border-radius: 12px;
			padding: 12px 14px;
			border: 1px solid rgba(37, 48, 70, 0.6);
			font-size: 13px;
			color: var(--muted);
		}

		.rule-block strong {
			display: block;
			font-size: 12px;
			text-transform: uppercase;
			letter-spacing: 0.08em;
			color: var(--text);
			margin-bottom: 6px;
		}

		.permissions-list {
			display: flex;
			flex-wrap: wrap;
			gap: 8px;
		}

		.perm-chip {
			padding: 4px 8px;
			border-radius: 8px;
			font-size: 11px;
			border: 1px solid rgba(37, 48, 70, 0.8);
			color: var(--muted);
		}

		.perm-allow {
			background: rgba(61, 220, 151, 0.14);
			color: var(--success);
			border-color: rgba(61, 220, 151, 0.4);
		}

		.perm-deny {
			background: rgba(255, 107, 107, 0.14);
			color: var(--danger);
			border-color: rgba(255, 107, 107, 0.4);
		}

		.perm-inherit {
			background: rgba(148, 163, 184, 0.12);
			color: var(--muted);
			border-color: rgba(148, 163, 184, 0.4);
		}

		.rule-actions {
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 12px;
			flex-wrap: wrap;
		}

		.switch {
			display: inline-flex;
			align-items: center;
			gap: 10px;
			font-size: 13px;
		}

		.switch input {
			appearance: none;
			width: 42px;
			height: 24px;
			background: rgba(37, 48, 70, 0.8);
			border-radius: 999px;
			position: relative;
			cursor: pointer;
			border: 1px solid rgba(37, 48, 70, 0.8);
		}

		.switch input::after {
			content: "";
			position: absolute;
			width: 18px;
			height: 18px;
			border-radius: 50%;
			background: #0b0e14;
			top: 2px;
			left: 2px;
			transition: transform 0.2s ease, background 0.2s ease;
			box-shadow: 0 2px 6px rgba(0, 0, 0, 0.5);
		}

		.switch input:checked {
			background: rgba(61, 220, 151, 0.4);
			border-color: rgba(61, 220, 151, 0.8);
		}

		.switch input:checked::after {
			transform: translateX(18px);
			background: var(--success);
		}

		.muted {
			color: var(--muted);
			font-size: 13px;
		}

		.empty-state {
			padding: 24px;
			text-align: center;
			color: var(--muted);
			border: 1px dashed rgba(37, 48, 70, 0.8);
			border-radius: 16px;
		}

		.drawer {
			position: fixed;
			inset: 0 0 0 auto;
			width: min(520px, 100%);
			background: rgba(11, 15, 23, 0.98);
			border-left: 1px solid rgba(37, 48, 70, 0.8);
			transform: translateX(100%);
			transition: transform 0.25s ease;
			z-index: 30;
			padding: 24px;
			overflow-y: auto;
		}

		.drawer-header {
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 12px;
			margin-bottom: 16px;
		}

		.drawer h2 {
			margin: 0;
			font-size: 20px;
		}

		.drawer form {
			display: flex;
			flex-direction: column;
			gap: 14px;
		}

		.form-row {
			display: flex;
			flex-direction: column;
			gap: 6px;
		}

		.form-row label {
			font-size: 12px;
			text-transform: uppercase;
			letter-spacing: 0.08em;
			color: var(--muted);
		}

		.textarea {
			min-height: 80px;
			resize: vertical;
		}

		.form-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
			gap: 12px;
		}

		.permission-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 10px;
		}

		.permission-item {
			background: rgba(10, 14, 22, 0.7);
			border: 1px solid rgba(37, 48, 70, 0.6);
			border-radius: 12px;
			padding: 10px;
			display: flex;
			flex-direction: column;
			gap: 6px;
			font-size: 12px;
			color: var(--muted);
		}

		.permission-item select {
			background: rgba(12, 16, 26, 0.9);
		}

		.native-tools-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
			gap: 12px;
			margin-top: 12px;
		}

		.native-tool-card {
			background: rgba(10, 14, 22, 0.7);
			border: 1px solid rgba(37, 48, 70, 0.6);
			border-radius: 12px;
			padding: 12px;
			display: flex;
			flex-direction: column;
			gap: 8px;
			font-size: 12px;
			color: var(--muted);
		}

		.native-tool-card strong {
			color: var(--text);
			font-size: 13px;
		}

		.drawer-actions {
			display: flex;
			gap: 10px;
			flex-wrap: wrap;
		}

		.overlay {
			position: fixed;
			inset: 0;
			background: rgba(6, 8, 14, 0.6);
			opacity: 0;
			pointer-events: none;
			transition: opacity 0.25s ease;
			z-index: 25;
		}

		body.drawer-open .drawer {
			transform: translateX(0);
		}

		body.drawer-open .overlay {
			opacity: 1;
			pointer-events: auto;
		}

		.toast {
			position: fixed;
			bottom: 24px;
			left: 50%;
			transform: translateX(-50%);
			background: rgba(15, 20, 32, 0.95);
			border: 1px solid rgba(37, 48, 70, 0.8);
			padding: 12px 18px;
			border-radius: 999px;
			font-size: 13px;
			color: var(--text);
			opacity: 0;
			pointer-events: none;
			transition: opacity 0.2s ease, transform 0.2s ease;
			z-index: 40;
		}

		.toast.show {
			opacity: 1;
			transform: translate(-50%, -6px);
		}

		.toast.success {
			border-color: rgba(61, 220, 151, 0.5);
		}

		.toast.error {
			border-color: rgba(255, 107, 107, 0.6);
			color: var(--danger);
		}

		.audit-table {
			display: grid;
			gap: 8px;
		}

		.audit-row {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
			gap: 8px;
			padding: 10px 12px;
			border-radius: 12px;
			background: rgba(12, 16, 26, 0.7);
			border: 1px solid rgba(37, 48, 70, 0.6);
			font-size: 12px;
			color: var(--muted);
		}

		.audit-row strong {
			color: var(--text);
			font-weight: 600;
			font-size: 12px;
		}

		.is-hidden {
			display: none;
		}

		.reveal {
			opacity: 0;
			transform: translateY(8px);
			transition: opacity 0.5s ease, transform 0.5s ease;
		}

		body.is-ready .reveal {
			opacity: 1;
			transform: translateY(0);
		}

		@media (max-width: 900px) {
			.topbar-inner {
				flex-direction: column;
				align-items: flex-start;
			}

			.hero {
				grid-template-columns: 1fr;
			}
		}

		@media (max-width: 600px) {
			.nav {
				gap: 8px;
			}

			.rule-actions {
				flex-direction: column;
				align-items: flex-start;
			}
		}

		@media (prefers-reduced-motion: reduce) {
			* {
				transition-duration: 0.01ms !important;
				animation-duration: 0.01ms !important;
			}
		}
	</style>
</head>
<body>
	<header class="topbar">
		<div class="topbar-inner">
			<div class="brand">Armour Control Plane</div>
			<nav class="nav">
				<a href="#overview">Overview</a>
			</nav>
			<div class="status-pill"><span class="status-dot"></span>Proxy online</div>
		</div>
	</header>

	<main class="app">
		<section id="overview" class="section reveal">
			<div class="hero">
				<div class="card">
					<h1>Security control for MCP tools</h1>
					<p>Armour sits between Claude Code and your MCP servers, enforcing tool-level rules, logging intent, and keeping your stack in a safe state.</p>
					<div class="hero-actions">
						<button class="btn btn-primary" id="refresh">Refresh</button>
						<a class="btn" href="https://github.com/fuushyn/armour" target="_blank" rel="noreferrer">Docs</a>
					</div>
				</div>
				<div class="card">
					<h3 style="margin-top: 0;">Live status</h3>
					<div class="stat-sub" id="last-refresh">Last refreshed: --</div>
					<div class="muted" style="margin-top: 12px;">Servers: <span id="server-count">0</span></div>
				</div>
			</div>

			<div class="stat-grid">
				<div class="card stat-card">
					<div class="stat-label">Blocked calls</div>
					<div class="stat-value" id="blocked-count">0</div>
					<div class="stat-sub">Total destructive attempts blocked</div>
				</div>
				<div class="card stat-card">
					<div class="stat-label">Allowed calls</div>
					<div class="stat-value" id="allowed-count">0</div>
					<div class="stat-sub">Proxy-approved tool calls</div>
				</div>
				<div class="card stat-card">
					<div class="stat-label">Block rate</div>
					<div class="stat-value" id="block-rate">0%</div>
					<div class="stat-sub">Share of calls blocked</div>
				</div>
				<div class="card stat-card">
					<div class="stat-label">Unique blocked tools</div>
					<div class="stat-value" id="unique-blocked">0</div>
					<div class="stat-sub">Distinct tools denied</div>
				</div>
			</div>

			<div class="card">
				<div class="section-header">
					<h2 class="section-title">Connected servers</h2>
					<div class="badge badge-ok" id="server-status">Running</div>
				</div>
				<div class="server-toolbar">
					<div>
						<div class="small-label">Registry</div>
						<div class="rule-desc" id="server-config-path">Detecting registry...</div>
					</div>
					<div class="rule-controls">
						<button class="btn btn-ghost" type="button" id="server-refresh">Reload</button>
					</div>
				</div>
				<div class="server-list" id="server-list">
					<div class="empty-state">Loading servers...</div>
				</div>
			</div>
		</section>

	</main>

	<div class="toast" id="toast"></div>

	<script>
		const state = {
			servers: [],
			registryPath: ''
		};

		const toast = document.getElementById('toast');

		function showToast(message, type) {
			toast.textContent = message;
			toast.className = 'toast show' + (type ? ' ' + type : '');
			setTimeout(() => {
				toast.className = 'toast';
			}, 2800);
		}

		function fetchJSON(url, options) {
			return fetch(url, options).then((res) => {
				if (!res.ok) {
					throw new Error('HTTP ' + res.status);
				}
				return res.json();
			});
		}

		function updateLastRefresh() {
			const stamp = new Date().toLocaleTimeString();
			document.getElementById('last-refresh').textContent = 'Last refreshed: ' + stamp;
		}

		function loadStats() {
			return fetchJSON('/api/stats')
				.then((data) => {
					document.getElementById('blocked-count').textContent = data.blocked_calls_total || 0;
					document.getElementById('allowed-count').textContent = data.allowed_calls_total || 0;
					document.getElementById('block-rate').textContent = (data.block_rate || 0).toFixed(1) + '%';
					document.getElementById('unique-blocked').textContent = data.unique_blocked_tools || 0;
				});
		}

		function loadServers() {
			return fetchJSON('/api/servers')
				.then((data) => {
					state.servers = data.servers || [];
					state.registryPath = data.path || '';
					document.getElementById('server-count').textContent = state.servers.length;
					renderRegistryPath();
					renderServers();
				});
		}

		function renderRegistryPath() {
			const pathEl = document.getElementById('server-config-path');
			if (!pathEl) {
				return;
			}
			if (state.registryPath) {
				pathEl.textContent = state.registryPath;
			} else {
				pathEl.textContent = 'Not persisted — start proxy with -config to write servers.json';
			}
		}

		function renderServers() {
			const container = document.getElementById('server-list');
			container.innerHTML = '';

			if (state.servers.length === 0) {
				container.innerHTML = '<div class="empty-state">No servers configured.</div>';
				return;
			}

			state.servers.forEach((server) => {
				const item = document.createElement('div');
				item.className = 'server-item';
				const transport = server.transport || 'unknown';
				const target = transport === 'stdio'
					? [server.command, ...(server.args || [])].filter(Boolean).join(' ')
					: (server.url || server.command || '');
				const summary = transport + ' — ' + (target || 'not configured');
				item.innerHTML =
					'<div>' +
						'<h3>' + escapeHTML(server.name) + '</h3>' +
				'<p>' + escapeHTML(summary) + '</p>' +
			'</div>' +
			'<span class="badge badge-ok">' + escapeHTML(transport.toUpperCase()) + '</span>';
			container.appendChild(item);
		});
	}

		function escapeHTML(text) {
			const div = document.createElement('div');
			div.textContent = text;
			return div.innerHTML;
		}

		document.getElementById('server-refresh').addEventListener('click', () => {
			loadServers()
				.then(() => showToast('Servers refreshed', 'success'))
				.catch((err) => showToast('Failed to reload servers: ' + err.message, 'error'));
		});

		document.getElementById('refresh').addEventListener('click', () => {
			Promise.all([loadStats(), loadServers()])
				.then(updateLastRefresh)
				.catch((err) => showToast('Refresh failed: ' + err.message, 'error'));
		});

		Promise.all([loadStats(), loadServers()])
			.then(updateLastRefresh)
			.catch((err) => showToast('Load failed: ' + err.message, 'error'));

		setInterval(() => {
			loadStats();
		}, 5000);

		document.body.classList.add('is-ready');
	</script>
</body>
</html>
`
}
