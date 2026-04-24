# Six-Agent Review Findings Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all HIGH/CRITICAL issues found by the six-agent review across backend API consistency, OpenAPI spec accuracy, frontend accessibility, and frontend UX bugs.

**Architecture:** Backend tasks are isolated handler/spec fixes in the Go repo (`/Users/fan/GolangProjects/ControlHub`). Frontend tasks are isolated component fixes in the Next.js repo (`/Users/fan/JsProjects/ControlHub`). No cross-repo dependencies between tasks.

**Tech Stack:** Go 1.26 + chi router (backend), Next.js 16 + React + shadcn/ui + ReactFlow (frontend)

---

## File Structure

### Backend (repo: `/Users/fan/GolangProjects/ControlHub`)
- **Modify:** `internal/api/audit_handler.go` — 2 `http.Error` → `writeJSONError`
- **Modify:** `internal/api/dictionary_handler.go` — 8 `http.Error` → `writeJSONError`
- **Modify:** `internal/api/auth_handler.go` — stop leaking raw error on 500
- **Modify:** `internal/openapi/openapi.yaml` — add `/resource-subtypes`, fix `archivedBy` type, add `profileSummary`
- **Modify:** `internal/api/resource_handler_test.go` — test the fixed error responses

### Frontend (repo: `/Users/fan/JsProjects/ControlHub`)
- **Modify:** `components/resources/edit-resource-sheet.tsx` — `window.confirm` → `AlertDialog`
- **Modify:** `components/blocks/topology-panel.tsx` — nodeTypes stability + popup accessibility + duplicate controls
- **Modify:** `messages/en.json` — add unsaved-changes confirmation strings
- **Modify:** `messages/zh-CN.json` — add unsaved-changes confirmation strings

---

## Task 1: Replace `http.Error` with `writeJSONError` in audit_handler.go

**Files:**
- Modify: `internal/api/audit_handler.go:28`
- Modify: `internal/api/audit_handler.go:48`
- Test: `internal/api/resource_handler_test.go`

Two `http.Error()` calls return plain text instead of JSON, inconsistent with the rest of the API.

The `TestServer` in `internal/api/test_server.go` uses `fakeAuditRepo{}` — a hardcoded struct with no error override fields. To test the error path, add an `errOnList` flag to the fake.

- [ ] **Step 1: Add error-injection support to `fakeAuditRepo`**

In `internal/api/test_server.go`, update `fakeAuditRepo`:

```go
// BEFORE:
type fakeAuditRepo struct{}

// AFTER:
type fakeAuditRepo struct {
	errOnList             error
	errOnListByResourceID error
}
```

Update the two methods to respect the error fields:

```go
func (f fakeAuditRepo) ListAuditEvents(ctx context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error) {
	if f.errOnList != nil {
		return nil, 0, f.errOnList
	}
	// ... rest of existing method body unchanged
}

func (f fakeAuditRepo) ListByResourceID(resourceID uint64) ([]model.AuditEvent, error) {
	if f.errOnListByResourceID != nil {
		return nil, f.errOnListByResourceID
	}
	return []model.AuditEvent{{ID: 1, ActorUserID: 1, TargetResourceID: &resourceID, EventType: "resource.updated", Result: "success", CreatedAt: time.Date(2026, 4, 11, 21, 0, 0, 0, time.UTC)}}, nil
}
```

Update `NewTestServer()` to use the modified fake (no change needed at the call site since zero-value `fakeAuditRepo{}` behaves the same).

- [ ] **Step 2: Write the failing test**

Add test cases to `internal/api/resource_handler_test.go`:

```go
func TestAuditHandlerJSONErrors(t *testing.T) {
	t.Run("audit list returns JSON on service failure", func(t *testing.T) {
		ts := newTestServerWithAuditError(fmt.Errorf("db lost"), nil)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/audit-events?page=1&pageSize=10", nil)
		ts.Router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "internal_error", body.Error)
	})

	t.Run("resource audit list returns JSON on service failure", func(t *testing.T) {
		ts := newTestServerWithAuditError(nil, fmt.Errorf("db lost"))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/resources/1/audit-events", nil)
		ts.Router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "internal_error", body.Error)
	})
}
```

Add a helper in `resource_handler_test.go`:

```go
func newTestServerWithAuditError(listErr, listByResourceIDErr error) *TestServer {
	return newTestServerWithDeps(func(deps *Dependencies) {
		deps.AuditService = service.NewAuditService(fakeAuditRepo{
			errOnList:             listErr,
			errOnListByResourceID: listByResourceIDErr,
		})
	})
}
```

This requires `NewTestServer()` to be refactored to expose `Dependencies` — or, simpler, add a `newTestServerWithDeps` helper. Check if `NewTestServer` already accepts options or if you need to add a small constructor variant. If the refactor is too invasive, an alternative is to create a standalone handler test that constructs just the audit handler with a failing service directly:

```go
func TestAuditHandlerJSONErrors(t *testing.T) {
	t.Run("audit list returns JSON on service failure", func(t *testing.T) {
		svc := service.NewAuditService(failingAuditRepo{})
		handler := handleListAuditEvents(svc)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/audit-events?page=1&pageSize=10", nil)
		handler.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "internal_error", body.Error)
	})
}

type failingAuditRepo struct{}

func (failingAuditRepo) ListAuditEvents(_ context.Context, _ model.AuditListQuery) ([]model.AuditEvent, int, error) {
	return nil, 0, fmt.Errorf("db connection lost")
}
func (failingAuditRepo) ListByResourceID(_ uint64) ([]model.AuditEvent, error) {
	return nil, fmt.Errorf("db connection lost")
}
```

Use the standalone approach (second option) — it avoids modifying `TestServer` and is simpler.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/api -v -run TestAuditHandlerJSONErrors`
Expected: FAIL — response Content-Type is `text/plain; charset=utf-8`, not `application/json`.

- [ ] **Step 4: Fix audit_handler.go line 28**

Change `internal/api/audit_handler.go:28` from:
```go
http.Error(w, err.Error(), http.StatusInternalServerError)
```
to:
```go
writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
```

- [ ] **Step 5: Fix audit_handler.go line 48**

Change `internal/api/audit_handler.go:48` from:
```go
http.Error(w, err.Error(), http.StatusInternalServerError)
```
to:
```go
writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/api -v -run TestAuditHandlerJSONErrors`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/api/audit_handler.go internal/api/resource_handler_test.go
git commit -m "fix: return JSON errors from audit handlers instead of plain text"
```

---

## Task 2: Replace `http.Error` with `writeJSONError` in dictionary_handler.go

**Files:**
- Modify: `internal/api/dictionary_handler.go:19,33,47,61,75,89,103,117,123`

Eight `http.Error()` calls across 7 dictionary handlers + 1 bad-request handler return plain text.

Testing strategy: construct handlers directly with a failing service, same as Task 1. Dictionary services use simple `List() ([]T, error)` signatures. Create a generic failing repo that returns errors for any dictionary service.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/resource_handler_test.go`. For the 7 dictionary handlers, test one representative (environments) to prove the pattern, plus the subtypes 400 case which doesn't need a failing service:

```go
func TestDictionaryHandlerJSONErrors(t *testing.T) {
	t.Run("environments returns JSON on service failure", func(t *testing.T) {
		svc := service.NewEnvironmentService(failingEnvRepo{})
		handler := handleListEnvironments(svc)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/environments", nil)
		handler.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "internal_error", body.Error)
	})

	t.Run("resource-subtypes returns JSON 400 when missing resourceType", func(t *testing.T) {
		svc := service.NewResourceSubtypeService()
		handler := handleListResourceSubtypes(svc)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/resource-subtypes", nil)
		handler.ServeHTTP(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "validation_failed", body.Error)
		assert.Contains(t, body.Message, "resourceType")
	})
}

type failingEnvRepo struct{}

func (failingEnvRepo) FindAll() ([]model.Environment, error) {
	return nil, fmt.Errorf("db connection lost")
}
```

The `failingEnvRepo` must implement whatever interface `service.NewEnvironmentService` expects. Check `internal/service/` for the interface — likely `EnvironmentRepository` with a `FindAll() ([]model.Environment, error)` method.

The same pattern applies for all 7 handlers — once the fix is in place, the test for environments proves it. The subtypes test is separate because it tests the validation path, not the service-error path.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api -v -run TestDictionaryHandlerJSONErrors`
Expected: FAIL — Content-Type is `text/plain; charset=utf-8`, not JSON.

- [ ] **Step 3: Replace all 8 `http.Error` calls in dictionary_handler.go**

Each of the 7 handlers (environments, owners, roles, lifecycleStatuses, healthStatuses, resourceTypes, relationTypes) has the same pattern on its error branch. Replace each:

```go
// BEFORE (repeated 7 times):
http.Error(w, err.Error(), http.StatusInternalServerError)

// AFTER:
writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
```

For the subtypes handler at line 117, change:
```go
// BEFORE:
http.Error(w, "resourceType query parameter is required", http.StatusBadRequest)

// AFTER:
writeJSONError(w, http.StatusBadRequest, "validation_failed", "resourceType query parameter is required")
```

And the subtypes handler service error at line 123:
```go
// BEFORE:
http.Error(w, err.Error(), http.StatusInternalServerError)

// AFTER:
writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api -v -run TestDictionaryHandlerErrorResponses`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/api/dictionary_handler.go internal/api/resource_handler_test.go
git commit -m "fix: return JSON errors from dictionary handlers instead of plain text"
```

---

## Task 3: Fix auth_handler.go raw error leak on 500

**Files:**
- Modify: `internal/api/auth_handler.go:31`

Line 31 passes `err.Error()` as the message for 500 responses, which can leak internal stack details.

- [ ] **Step 1: Fix auth_handler.go line 31**

Change:
```go
writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
```
to:
```go
writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
```

This matches the pattern used in `writeServiceError()` (resource_handler.go:285) which already hides internal details for unknown errors.

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/auth_handler.go
git commit -m "fix: stop leaking raw error message in auth 500 response"
```

---

## Task 4: Update OpenAPI spec — add `/resource-subtypes`, fix `archivedBy`, add `profileSummary`

**Files:**
- Modify: `internal/openapi/openapi.yaml`

Three spec gaps:
1. `/resource-subtypes` endpoint is implemented in the router but missing from the spec
2. `archivedBy` is typed as `string` in the spec but the Go model uses `*uint64`
3. `profileSummary` field exists in the API response but not in the spec

- [ ] **Step 1: Add `/resource-subtypes` endpoint to OpenAPI spec**

After the existing `/resource-types` path block, add:

```yaml
  /resource-subtypes:
    get:
      summary: List resource subtypes for a given resource type
      operationId: listResourceSubtypes
      parameters:
        - in: query
          name: resourceType
          required: true
          schema:
            $ref: "#/components/schemas/ResourceType"
          description: The resource type to list subtypes for
      responses:
        "200":
          description: Resource subtypes
          content:
            application/json:
              schema:
                type: object
                required: [resourceType, subtypes]
                properties:
                  resourceType:
                    type: string
                  subtypes:
                    type: array
                    items:
                      $ref: "#/components/schemas/DictionaryItem"
        "400":
          description: Missing or invalid resourceType parameter
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
```

- [ ] **Step 2: Fix `archivedBy` type from `string` to `integer`**

Find the `archivedBy` property under the Resource schema (around line 1106-1109) and change:

```yaml
        archivedBy:
          type: string
          nullable: true
          description: User or system that archived the resource
```
to:
```yaml
        archivedBy:
          type: integer
          format: int64
          nullable: true
          description: ID of the user or system that archived the resource
```

- [ ] **Step 3: Add `profileSummary` to the Resource schema**

After the `archiveReason` property (around line 1113), add:

```yaml
        profileSummary:
          $ref: "#/components/schemas/ProfileSummary"
          nullable: true
          description: Embedded profile summary (present when includeProfile=true query param is set)
```

Then add the `ProfileSummary` schema to the components/schemas section:

```yaml
    ProfileSummary:
      type: object
      description: A brief summary of a resource's profile, included inline on list responses
      additionalProperties: true
      properties:
        engine:
          type: string
        version:
          type: string
        host:
          type: string
        port:
          type: integer
        role:
          type: string
```

- [ ] **Step 4: Validate the OpenAPI spec**

Run: `make openapi-validate`
Expected: No validation errors.

- [ ] **Step 5: Commit**

```bash
git add internal/openapi/openapi.yaml
git commit -m "fix: add /resource-subtypes to OpenAPI spec, fix archivedBy type, add profileSummary"
```

---

## Task 5: Replace `window.confirm` with localized AlertDialog (frontend)

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/components/resources/edit-resource-sheet.tsx:174-186`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/zh-CN.json`

`window.confirm` is not localizable, blocks the UI thread, and doesn't match the design system. The `AlertDialog` component already exists at `components/ui/alert-dialog.tsx` and is used in `resource-relation-panel.tsx`.

- [ ] **Step 1: Add i18n keys for the unsaved-changes dialog**

In `messages/en.json`, add under the `common` section (after the existing `actions` block):

```json
"unsavedChanges": {
  "title": "Discard changes?",
  "description": "You have unsaved changes that will be lost.",
  "discard": "Discard"
}
```

In `messages/zh-CN.json`, add the same structure:

```json
"unsavedChanges": {
  "title": "放弃更改？",
  "description": "你有未保存的更改，离开后将会丢失。",
  "discard": "放弃"
}
```

- [ ] **Step 2: Replace `window.confirm` with state-driven AlertDialog**

In `edit-resource-sheet.tsx`, add a state variable for the pending close:

```tsx
const [pendingClose, setPendingClose] = useState(false);
```

Replace the `handleOpenChange` callback (lines 175-186):

```tsx
// BEFORE:
const handleOpenChange = useCallback(
  (nextOpen: boolean) => {
    if (!nextOpen && isDirty) {
      const confirmed = window.confirm(
        "You have unsaved changes. Discard?",
      );
      if (!confirmed) return;
    }
    onOpenChange(nextOpen);
  },
  [isDirty, onOpenChange],
);

// AFTER:
const handleOpenChange = useCallback(
  (nextOpen: boolean) => {
    if (!nextOpen && isDirty) {
      setPendingClose(true);
      return;
    }
    onOpenChange(nextOpen);
  },
  [isDirty, onOpenChange],
);

const handleDiscardConfirm = useCallback(() => {
  setPendingClose(false);
  onOpenChange(false);
}, [onOpenChange]);

const handleDiscardCancel = useCallback(() => {
  setPendingClose(false);
}, []);
```

Add the AlertDialog import at the top of the file:

```tsx
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
```

Add the AlertDialog component inside the Sheet's return JSX, at the end before the closing fragment:

```tsx
<AlertDialog open={pendingClose} onOpenChange={(open) => { if (!open) handleDiscardCancel(); }}>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>{t("common.unsavedChanges.title")}</AlertDialogTitle>
      <AlertDialogDescription>
        {t("common.unsavedChanges.description")}
      </AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>{t("common.actions.cancel")}</AlertDialogCancel>
      <AlertDialogAction onClick={handleDiscardConfirm}>
        {t("common.unsavedChanges.discard")}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

- [ ] **Step 3: Verify in browser**

Run: `cd /Users/fan/JsProjects/ControlHub && pnpm dev`
Open http://localhost:3000, edit a resource, make changes, click close — should see styled AlertDialog, not browser confirm. Both English and Chinese should work.

- [ ] **Step 4: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub
git add components/resources/edit-resource-sheet.tsx messages/en.json messages/zh-CN.json
git commit -m "fix: replace window.confirm with localized AlertDialog for unsaved changes"
```

---

## Task 6: Fix nodeTypes stability and popup accessibility in topology-panel.tsx

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`

Three issues in one file:
1. `nodeTypes` defined inside render causes ReactFlow remounts
2. Detail popup missing `role="dialog"` and `aria-modal`
3. Duplicate controls when topology is expanded

- [ ] **Step 1: Move nodeTypes outside the component**

The nodeTypes object is defined with `useMemo` at line 308 with dependencies `[t, getTypeLabel, getRoleLabel, isDatabase]`. These dependencies change, defeating the memoization. Move the components outside `TopologyPanelInner` and pass data via props/context instead.

Add before the `TopologyPanelInner` component definition:

```tsx
// Custom node components defined outside render to prevent ReactFlow remounts
const TopologyNodeComponent = ({ data }: { data: TopologyNodeData }) => {
  const { t } = useTranslations();
  // ... move the existing node JSX here, replacing closures with data props
};
```

However, since the node components use `t`, `getRoleLabel`, `getTypeLabel`, `isDatabase` from the parent scope, the cleanest fix is to move nodeTypes to a stable reference by extracting the component definitions outside `TopologyPanelInner` and using ReactFlow's built-in data passing.

The minimal fix: use `useMemo` with an empty dependency array by extracting `t`, `getRoleLabel`, `getTypeLabel` into the node data instead of closing over them:

```tsx
// Line ~308: Replace the existing useMemo
const nodeTypes = useMemo(
  () => ({
    topologyNode: TopologyNodeComponent,
    topologyGroup: TopologyGroupComponent,
  }),
  [], // stable — no dependencies
);
```

This requires:
1. Extract `TopologyNodeComponent` as a standalone component above `TopologyPanelInner`
2. Pass `roleLabel`, `typeLabel` via `data` props instead of closing over parent functions
3. Use `useTranslations()` directly inside the node component (it's a hook, safe to call)

The existing node data type `TopologyNodeData` may need `roleLabel?: string` and `typeLabel?: string` added. These should be populated when building the nodes in `buildNodesFromTopology()`.

- [ ] **Step 2: Add accessibility attributes to the detail popup**

At line 513 (overlay div) and line 519 (popup content), add ARIA attributes:

```tsx
// Line 513 — overlay:
<div
  className="fixed inset-0 z-50"
  onClick={() => setSelectedNodePopup(null)}
  onKeyDown={(e) => { if (e.key === "Escape") setSelectedNodePopup(null); }}
  role="presentation"
  data-testid="topology-node-popup-overlay"
>

// Line 519 — popup content:
<div
  className="absolute rounded-xl border border-border bg-card shadow-lg p-4 min-w-[280px] max-w-[340px]"
  style={{ left: px, top: py }}
  onClick={(e) => e.stopPropagation()}
  role="dialog"
  aria-modal="true"
  aria-label={`${d.displayName || d.name} details`}
  data-testid="topology-node-popup"
>
```

- [ ] **Step 3: Fix duplicate topology controls when expanded**

At line 741, the controls render unconditionally. When `expanded` is true, they also render inside the overlay header. Guard the inline controls:

```tsx
// Line 741 — change from:
{renderControls()}

// to:
{!expanded && renderControls()}
```

- [ ] **Step 4: Verify in browser**

Run: `cd /Users/fan/JsProjects/ControlHub && pnpm dev`
1. Navigate to a resource with topology — nodes should render without ReactFlow remount warnings
2. Click a node — popup should have `role="dialog"` and `aria-modal`
3. Click expand — controls should appear only once, not twice
4. Press Escape — popup should close

- [ ] **Step 5: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub
git add components/blocks/topology-panel.tsx
git commit -m "fix: stabilize nodeTypes, add popup accessibility, fix duplicate controls"
```

---

## Task 7: Add tabIndex and aria-label to topology nodes

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx:325-329`

Topology nodes lack keyboard focus and screen reader labels.

- [ ] **Step 1: Add tabIndex and aria-label to the node wrapper div**

In the `TopologyNodeComponent` JSX (around line 325), change the node wrapper div:

```tsx
// BEFORE:
<div
  data-testid={`topology-node-${data.id}`}
  data-is-root={data.isRoot ? "true" : "false"}
  data-topology-role={data.topologyRole}

// AFTER:
<div
  data-testid={`topology-node-${data.id}`}
  data-is-root={data.isRoot ? "true" : "false"}
  data-topology-role={data.topologyRole}
  tabIndex={0}
  role="button"
  aria-label={`${data.displayName || data.name}, ${data.healthStatus || "unknown status"}`}
  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); data.onNodeClick?.(data.id); } }}
  className={cn(
    "relative flex flex-col items-center rounded-lg border-2 bg-card p-2 transition-shadow",
    roleBorder,
    roleBg,
    statusStyle.border,
    isDb ? "min-w-[120px]" : "min-w-[100px]",
    data.isRoot && "ring-2 ring-primary/30",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 focus-visible:ring-offset-1",
  )}
```

Note: The `className` shown above is illustrative. When implementing, copy the exact existing className from the node wrapper div and append only the `focus-visible:*` utilities. Check what `onClick` does on the node — if it's inline or references a callback, replicate that exact call in `onKeyDown`.

Also add `aria-label` to the group node component (`topologyGroup`) with its zone name.

- [ ] **Step 2: Verify keyboard navigation**

In browser: Tab through the topology — each node should receive focus with a visible ring. Screen reader should announce the node name and status.

- [ ] **Step 3: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub
git add components/blocks/topology-panel.tsx
git commit -m "fix: add keyboard accessibility to topology nodes"
```

---

## Task Summary

| Task | Scope | Files | Risk |
|------|-------|-------|------|
| 1 | Backend: audit_handler JSON errors | 2 files | LOW — simple string replacement |
| 2 | Backend: dictionary_handler JSON errors | 2 files | LOW — same pattern 8 times |
| 3 | Backend: auth error leak | 1 file | LOW — one-line change |
| 4 | Backend: OpenAPI spec gaps | 1 file | LOW — spec-only, no code change |
| 5 | Frontend: window.confirm → AlertDialog | 3 files | MEDIUM — adds state management |
| 6 | Frontend: nodeTypes + popup a11y + dupes | 1 file | MEDIUM — refactor extraction |
| 7 | Frontend: topology keyboard a11y | 1 file | LOW — adds attributes |

**Execution order:** Tasks 1-4 (backend) can run in any order, are independent. Tasks 5-7 (frontend) should run sequentially since tasks 6 and 7 both modify `topology-panel.tsx`.

**Subagent notes:** Each task is self-contained with exact code. Backend tasks touch different handler files. Frontend tasks 6+7 touch the same file — run sequentially.
