# Console Preferences Design

## 1. Goal

Refine the topbar preference controls for the ControlHub frontend so that:

- language switching is faster than the current dropdown
- theme switching stays lightweight
- primary accent color becomes user-selectable without corrupting business status semantics

This is a UX and theming refinement only.
It does not add new business capability, does not change backend contracts, and does not widen CMDB scope.

## 2. Scope

### Included

- replace the language dropdown with a two-option direct switch
- keep theme switching in the topbar
- add a topbar accent-color picker
- persist language, theme, and accent preferences locally
- add a matching appearance section in settings as a secondary control surface
- keep light and dark mode compatible with the same accent-color model

### Excluded

- arbitrary color picker
- per-page custom themes
- route-based locale prefixes
- changing success, warning, error, or info semantic colors
- redesigning the whole shell layout

## 3. User-Approved Interaction Direction

### Language

Use a topbar `中 / EN` segmented control.

Reason:

- only two locales exist in phase 1
- segmented control is faster and clearer than a select
- it consumes less cognitive load than a dropdown for a binary choice

### Theme

Keep theme as a single compact topbar control.

Behavior:

- click cycles `light -> dark -> system -> light`
- stays lightweight and does not become a dropdown

### Accent color

Use a small topbar trigger that opens a compact popover with four fixed presets.

User-approved preset count:

- 4 fixed presets

Required preset:

- purple must be included

Recommended preset set:

- blue
- purple
- emerald
- amber

## 4. Approaches Considered

### Approach A: Topbar-only controls

- language segmented control in topbar
- theme control in topbar
- accent popover in topbar
- settings page only reflects current state passively

Pros:

- fastest daily usage
- smallest implementation

Cons:

- long-term settings discoverability is weaker
- topbar owns all preference logic

### Approach B: Topbar quick controls plus settings fallback

- topbar contains quick language, theme, and accent controls
- settings page contains an `Appearance` section showing the same preference state

Pros:

- daily interactions stay fast
- settings remains the canonical place for user preferences
- future growth stays manageable

Cons:

- requires shared preference state across two entry points

### Approach C: Minimal topbar, settings-heavy

- language in topbar
- theme and accent only in settings

Pros:

- cleanest topbar

Cons:

- too slow for experimentation
- does not match the user’s desired quick accent switching

### Recommendation

Use **Approach B**.

It keeps the topbar practical for frequent switching while preserving settings as the stable preference surface.

## 5. Visual and Product Rules

### Preference controls order

Topbar right-side controls should read:

- language segmented control
- theme button
- accent-color trigger

Then the existing quick action, notifications, and user menu continue after them.

### Accent-color boundary

Accent color affects only product emphasis, not semantic state colors.

Allowed to change:

- `ControlHub` wordmark emphasis
- brand/avatar accent treatment
- active nav state
- primary button color
- focus ring
- selected state border/background
- subtle interactive emphasis text

Must not change:

- success color
- warning color
- error color
- info color
- audit result semantics
- health-status semantics
- large neutral background layers
- table grayscale hierarchy

This prevents user preference from corrupting operator-facing meaning.

## 6. Technical Design

### Language model

- keep current cookie-backed locale strategy
- continue using `controlhub.locale`
- replace `LanguageSwitcher` select with a segmented control component
- values remain `zh-CN` and `en`

### Theme model

- keep current `next-themes` integration
- continue using `controlhub.theme`
- keep button-based cycle behavior

### Accent-color model

- add a new persisted preference key, e.g. `controlhub.accent`
- values are one of:
  - `blue`
  - `purple`
  - `emerald`
  - `amber`
- source of truth stays client-side for phase 1
- local persistence can use `localStorage`
- HTML root should receive a stable attribute such as `data-accent="purple"`

### Styling strategy

Do not implement accent switching with large class-variant branching across components.

Use CSS variables:

- keep semantic tokens such as `--primary`, `--ring`, `--sidebar-primary`
- derive those tokens from `data-accent`
- provide both light and dark values per accent preset

This keeps the shell maintainable and avoids component-level theme explosion.

### Component changes

- `components/settings/language-switcher.tsx`
  - replace select UI with segmented two-option control
- `components/settings/theme-toggle.tsx`
  - keep current compact button behavior
- add new accent control component, likely:
  - `components/settings/accent-switcher.tsx`
- `components/providers/app-providers.tsx`
  - include accent preference hydration
- `app/globals.css`
  - add accent preset token maps for light and dark mode
- `components/app-shell/topbar.tsx`
  - insert the new controls in the approved order
- `app/(console)/settings/page.tsx`
  - add `Appearance` section reflecting language, theme, and accent state

## 7. State and Data Flow

### Language

- user clicks `中` or `EN`
- locale cookie updates
- router refreshes
- server-rendered strings re-resolve from `next-intl`

### Theme

- user clicks theme button
- `next-themes` updates theme state
- root class changes

### Accent

- user opens accent popover
- selects one preset
- local preference updates
- root `data-accent` updates immediately
- CSS variables update without page reload

## 8. Testing and Validation

### Automated

- existing frontend test suite remains green
- add focused tests for:
  - language segmented control state
  - accent switcher interaction
  - preference persistence where practical

### Manual

Verify on:

- `/overview`
- `/resources`
- `/resources/{id}`
- `/audits`
- `/settings`

Verify for both `zh-CN` and `en`, and for `light` and `dark`.

Check specifically:

- topbar controls do not crowd each other
- accent presets update brand emphasis only
- semantic status colors remain unchanged
- focus ring changes with accent
- active nav state changes with accent
- purple preset remains legible in both themes

## 9. Risks and Guardrails

### Risk: Accent spills into semantic status colors

Guardrail:

- never bind health/audit semantic colors to accent tokens

### Risk: Topbar density becomes noisy

Guardrail:

- keep controls icon-sized or short-width only
- avoid text-heavy labels in the topbar

### Risk: Theme and accent hydration flicker

Guardrail:

- hydrate root theme/accent attributes early
- prefer CSS-variable token switching over rerender-heavy component logic

### Risk: Duplicate preference logic between topbar and settings

Guardrail:

- share one preference hook or provider
- settings is a secondary surface, not a separate state system

## 10. Implementation Order

1. Add accent preference model and root attribute handling
2. Add accent token maps in `globals.css`
3. Replace language dropdown with segmented control
4. Add accent popover control in topbar
5. Add settings `Appearance` section using the same state
6. Re-run browser validation in both locales and themes

