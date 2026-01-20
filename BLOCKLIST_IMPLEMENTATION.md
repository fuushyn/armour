# Database-Driven Blocklist System - Implementation Summary

## Overview

This document summarizes the complete implementation of a sophisticated, database-driven blocklist system for Armour MCP Proxy, replacing the hardcoded tool blocking patterns and policy modes.

## ✅ Completed Implementation

### Phase 1: Database Schema & Models ✓

**Files Created:**
- `server/blocklist.go` - Core types and models
- `server/database.go` - Database operations

**Components:**
- `BlocklistRule` struct with permissions matrix
- `Permissions` struct (8 MCP operations)
- `BlocklistCheckResult` for enforcement results
- `Permission` enum (allow/deny/inherit)

**Database Schema:**
- `blocklist_rules` table with full CRUD support
- Extended `audit_log` table with blocklist columns
- Proper indexes for performance

### Phase 2: Blocklist Middleware ✓

**File Created:**
- `server/blocklist_middleware.go` - Full enforcement engine

**Features:**
- Rule caching with 30-second TTL
- Regex-based pattern matching (fast)
- Semantic matching via Claude API (flexible)
- Permission evaluation (8 MCP operations)
- Tool-specific targeting
- Comprehensive error handling

**Methods:**
- `Check()` - Main validation endpoint
- `checkRegexRules()` - Pattern matching
- `checkSemanticRules()` - LLM integration
- `checkPermission()` - Permission validation
- `RefreshRulesCache()` - Cache invalidation

### Phase 3: Admin API Endpoints ✓

**File Modified:**
- `dashboard/server.go` - Added blocklist API

**Endpoints:**
- `GET /api/blocklist` - List all rules
- `POST /api/blocklist` - Create new rule
- `PUT /api/blocklist?id=X` - Update rule
- `DELETE /api/blocklist?id=X` - Delete rule
- Automatic cache refresh on modifications

### Phase 4: Web UI Implementation ✓

**File Modified:**
- `dashboard/server.go` - Added blocklist management page

**Features:**
- Full rule management interface at `/blocklist`
- Create/Edit modal form with:
  - Pattern/Topic input
  - Description
  - Action selector (Block/Allow)
  - Match type checkboxes (Regex/Semantic)
  - Tool multi-select
- Rule table with:
  - Pattern display
  - Action badges (red=block, green=allow)
  - Type badges (Regex/Semantic)
  - Enable/Disable toggles
  - Edit/Delete actions
- Real-time rule loading
- CRUD operations via JavaScript Fetch API

### Phase 5: Request Handler Integration ✓

**File Modified:**
- `server/stdio_server.go` - Wired blocklist checks into all handlers

**Handlers Protected:**
- `handleToolsList()` - tools/list permission check
- `handleToolsCall()` - tools/call permission check (primary)
- `handleResourcesList()` - resources/list permission check
- `handleResourcesRead()` - resources/read permission check
- `handlePromptsList()` - prompts/list permission check
- `handlePromptsGet()` - prompts/get permission check

**Integration Details:**
- Blocklist check happens BEFORE backend routing
- Automatic stats recording (blocked/allowed)
- Graceful error responses
- Nil safety checks

### Phase 6: Configuration & Migration ✓

**Files Created:**
- `server/migration_blocklist.go` - Automated migration functions

**Migration Features:**
- `MigrateHardcodedPatternsToBlocklist()` - Converts 15 destructive patterns
- `MigrateStrictModeToBlocklist()` - Creates strict mode equivalent
- `MigratePermissiveModeToBlocklist()` - Creates permissive mode equivalent
- `MigrateModerateModeToBlocklist()` - Documents moderate mode handling
- `PerformFullMigration()` - Runs all migrations
- `CheckIfMigrationNeeded()` - Detects empty blocklist table

### Phase 7: PolicyManager Deprecation ✓

**File Modified:**
- `server/policy_manager.go` - Added deprecation notices

**Deprecation Strategy:**
- Clear "DEPRECATED" markers in code comments
- Migration guide in documentation
- Will be removed in v2.0.0
- Backward compatibility maintained for now

### Testing ✓

**File Created:**
- `server/blocklist_test.go` - Comprehensive test suite

**Test Coverage:**
- CRUD operations (Create, Read, Update, Delete, Toggle)
- Permission evaluation logic
- Default permission generation
- Tool filtering logic
- Migration functions
- Concurrent database access

**Tests Included:**
- `TestBlocklistBasics()` - Full CRUD flow
- `TestPermissions()` - Permission enum handling
- `TestDefaultPermissions()` - Permission defaults
- `TestRuleAppliesToTool()` - Tool filtering
- `TestMigration()` - Full migration process
- `TestConcurrentAccess()` - Concurrent safety

## Architecture Overview

```
Claude Code (Client)
        ↓
   JSON-RPC Request
        ↓
StdioServer.handleXXX()
        ↓
BlocklistMiddleware.Check()
    ├─ Rule Caching (30s TTL)
    ├─ Regex Matching (Fast)
    ├─ Semantic Matching (Claude API)
    └─ Permission Evaluation
        ↓
    If Allowed → Route to Backend
    If Denied → Return Error Response
        ↓
   Stats Tracking (blocked/allowed)
```

## Database Schema

### blocklist_rules Table

```sql
CREATE TABLE blocklist_rules (
    id INTEGER PRIMARY KEY,
    pattern TEXT NOT NULL,
    description TEXT,
    action TEXT CHECK(action IN ('block', 'allow')),
    is_regex INTEGER DEFAULT 0,
    is_semantic INTEGER DEFAULT 1,
    tools TEXT DEFAULT '',

    -- Claude IAM-style permissions (8 operations)
    perm_tools_call TEXT DEFAULT 'deny',
    perm_tools_list TEXT DEFAULT 'allow',
    perm_resources_read TEXT DEFAULT 'deny',
    perm_resources_list TEXT DEFAULT 'allow',
    perm_resources_subscribe TEXT DEFAULT 'deny',
    perm_prompts_get TEXT DEFAULT 'deny',
    perm_prompts_list TEXT DEFAULT 'allow',
    perm_sampling TEXT DEFAULT 'deny',

    enabled INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Extended audit_log Columns

```sql
ALTER TABLE audit_log ADD COLUMN blocked INTEGER DEFAULT 0;
ALTER TABLE audit_log ADD COLUMN block_reason TEXT;
ALTER TABLE audit_log ADD COLUMN matched_pattern TEXT;
ALTER TABLE audit_log ADD COLUMN denied_operation TEXT;
ALTER TABLE audit_log ADD COLUMN rule_action TEXT;
```

## API Endpoints

### Blocklist Management

**List Rules**
```
GET /api/blocklist

Response:
{
  "count": 5,
  "rules": [
    {
      "id": 1,
      "pattern": "delete.*",
      "description": "Block deletion operations",
      "action": "block",
      "is_regex": true,
      "is_semantic": false,
      "tools": "",
      "enabled": true,
      "created_at": "2024-01-19T12:00:00Z"
    }
  ]
}
```

**Create Rule**
```
POST /api/blocklist

Request:
{
  "pattern": "delete.*",
  "description": "Block deletion operations",
  "action": "block",
  "is_regex": true,
  "is_semantic": false,
  "tools": "tool1, tool2"
}

Response: Created rule with ID and timestamps
```

**Update Rule**
```
PUT /api/blocklist?id=1

Request:
{
  "pattern": "delete.*",
  "description": "Updated description",
  "action": "block",
  "is_regex": true,
  "is_semantic": false,
  "tools": "tool1, tool2",
  "enabled": true
}

Response: Updated rule
```

**Delete Rule**
```
DELETE /api/blocklist?id=1

Response:
{
  "status": "deleted"
}
```

## UI Features

### Blocklist Management Page (`/blocklist`)

**Components:**
1. **Header** - Title, description, create button
2. **Rules Table** - Displays all rules with actions
3. **Modal Form** - Create/Edit rules with validation
4. **Navigation** - Links to dashboard, audit log, settings

**Styling:**
- Professional gradient background
- Color-coded badges (red=block, green=allow, blue=regex, purple=semantic)
- Responsive design
- Hover effects and transitions
- Modal with form validation

## Permission Model

### Available Permissions

| Operation | Default (Block) | Default (Allow) | Purpose |
|-----------|-----------------|-----------------|---------|
| tools_call | deny | allow | Execute tools |
| tools_list | allow | allow | List available tools |
| resources_read | deny | allow | Read resource contents |
| resources_list | allow | allow | List available resources |
| resources_subscribe | deny | allow | Subscribe to updates |
| prompts_get | deny | allow | Retrieve prompts |
| prompts_list | allow | allow | List prompts |
| sampling | deny | allow | Use sampling features |

### Permission Values

- `allow` - Explicitly allow the operation
- `deny` - Block the operation
- `inherit` - Use default (currently defaults to deny)

## Migration Guide

### For Users on Policy Modes

1. **Run Migration:**
   ```go
   err := PerformFullMigration(db)
   ```

2. **Rules Created:**
   - 15 destructive patterns (from hardcoded list)
   - 1 strict mode rule (disabled by default)
   - 1 permissive rule (disabled by default)

3. **Moderate Mode:**
   - Equivalent to having only destructive patterns enabled
   - Default behavior after migration

4. **Access Dashboard:**
   - Navigate to `/blocklist`
   - View and manage all rules
   - Create custom rules

## Code Organization

```
server/
├── blocklist.go                 # Types and models
├── blocklist_middleware.go      # Enforcement engine
├── blocklist_test.go            # Comprehensive tests
├── database.go                  # DB operations
├── migration_blocklist.go       # Migration functions
├── stdio_server.go              # Handler integration
├── policy_manager.go            # Deprecated (stub)
└── server.go                    # Schema initialization

dashboard/
└── server.go                    # API + UI handlers
```

## Performance Characteristics

- **Rule Caching:** 30-second TTL for minimal database hits
- **Regex Matching:** <1ms per rule (calculated)
- **Semantic Matching:** Depends on Claude API latency (~1-2s)
- **Database Ops:** Indexed queries for fast retrieval
- **Concurrent Access:** SQLite handles up to 10 concurrent writers with WAL

## Security Considerations

1. **Default Deny:** Permissions default to deny for safety
2. **Fail Open:** LLM checks fail open (allow) if Claude API is down
3. **Input Validation:** Regex patterns validated before use
4. **Error Handling:** Graceful degradation with fallbacks
5. **Audit Trail:** All blocklist matches logged

## Breaking Changes from Policy Modes

1. **UI Changes:**
   - Settings page no longer has policy mode selector
   - New `/blocklist` management page

2. **API Changes:**
   - `/api/policy` endpoint still exists but deprecated
   - New `/api/blocklist` endpoints are primary

3. **Configuration:**
   - `policy.mode` no longer affects enforcement
   - Blocklist rules are now the enforcement mechanism

4. **Stats Tracking:**
   - Blocking reasons now indicate `blocklist:pattern` instead of `policy_mode`

## Migration Checklist

- [x] Database schema with blocklist_rules table
- [x] Core models and types (BlocklistRule, Permissions, etc.)
- [x] Blocklist middleware with regex + semantic matching
- [x] Admin API (GET, POST, PUT, DELETE)
- [x] Web UI for rule management
- [x] Integration with all request handlers
- [x] Cache management (30s TTL)
- [x] Audit logging
- [x] Migration functions
- [x] PolicyManager deprecation notices
- [x] Comprehensive test suite
- [x] Documentation

## Next Steps (Optional Enhancements)

1. **UI Improvements:**
   - Permission editor in modal (currently auto-set)
   - Rule templates (common patterns)
   - Rule testing sandbox
   - Bulk rule import/export

2. **Performance:**
   - Rule compilation caching
   - Regex pattern optimization
   - Semantic matching result caching

3. **Advanced Features:**
   - Rule scheduling (enable/disable by time)
   - Rule versioning and rollback
   - Rule impact simulation
   - Advanced audit analysis

## Known Limitations

1. **Claude API Required:** Semantic matching requires valid API key
2. **Rule Order:** Rules evaluated by ID order (oldest first)
3. **Tool Matching:** Simple suffix matching, not full glob patterns
4. **Permissions:** Fixed to 8 standard MCP operations

## Support & Troubleshooting

### Common Issues

**Q: Rules not enforcing**
- Check if rules are enabled (`enabled = 1`)
- Verify rule pattern matches (use regex tester)
- Check cache TTL (rules cached for 30s)

**Q: Semantic matching not working**
- Verify Claude API key is configured
- Check Claude API status and rate limits
- Review API response logs

**Q: Permission denied on safe operations**
- Check if rule has correct permissions set
- Verify operation matches MCP method
- Review audit log for matched rule

## Version Information

- **Implementation Version:** 1.0.13
- **Target Armour Version:** 1.1.0+
- **Deprecated Component:** PolicyManager (removal planned for 2.0.0)
- **Breaking Changes:** Yes (policy mode selection removed)

## Support

For issues or questions:
1. Check `/blocklist` UI for rule status
2. Review audit logs for blocklist matches
3. Run migration: `PerformFullMigration(db)`
4. Check test suite: `go test ./server -v`
