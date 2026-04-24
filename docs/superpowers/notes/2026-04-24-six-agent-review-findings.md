# Six-Agent Review Findings (2026-04-24)

> Evidence-based review from Frontend Developer, UX Architect, UX Researcher, UI Designer, API Tester, and Evidence Collector agents.

## Consensus Issues (flagged by 2+ agents)

| # | Issue | Agents | Severity |
|---|-------|--------|----------|
| 1 | `window.confirm()` not localized, breaks design consistency | Frontend, UX Arc, UX Res, UI | HIGH |
| 2 | topology-panel.tsx 833 lines, needs decomposition | Frontend, UX Arc | HIGH |
| 3 | `as any` casts suppress TypeScript checking in forms | Frontend | CRITICAL |
| 4 | `nodeTypes` defined inside render causes ReactFlow remounts | Frontend | CRITICAL |
| 5 | Topology nodes lack keyboard accessibility (tabIndex, aria-label, focus ring) | Frontend, UI | HIGH |
| 6 | Detail popup missing `role="dialog"`, focus trap, aria-modal | Frontend, UI | HIGH |
| 7 | Severity indicators use color only, no aria-label | UI | HIGH |
| 8 | `/resource-subtypes` endpoint missing from OpenAPI spec | API | HIGH |
| 9 | 10 `http.Error()` calls return plain text, not JSON | API | HIGH |
| 10 | Create form bypasses react-hook-form `handleSubmit` | Frontend, UX Arc, UX Res | HIGH |
| 11 | Dual navigation model (Sheet vs Page) confuses users | UX Arc, UX Res | HIGH |
| 12 | LabelsEditor has hardcoded English + allows empty/duplicate keys | UX Arc, UX Res | MEDIUM |

## Frontend Developer (8C + 10I + 9O)

- C1: nodeTypes reference instability → ReactFlow full remount
- C2: unsafe `as HTMLElement` cast on event.currentTarget for popup positioning
- C3: popup position not updated on scroll/resize
- C4/C5: `as any` on zodResolver + manual validation bypass
- C6: `window.confirm` hardcoded English, blocks UI thread
- C7: api-client catch block silently swallows JSON parse errors
- C8: `router.push` in URL sync causes full page navigation (should be `replace`)
- I1: topology-panel 833 lines → extract 5 components
- I2: create-resource-sheet 746 lines → extract sections
- I4: profile-field-registry vs create sheet have two different validation strategies
- I5: `locale as never` bypasses type checking
- I6: unsafe type assertions on URL parameters
- I9: highlightNode creates new object per node, no clear mechanism

## UX Architect (5C + 7M)

- No mobile navigation (sidebar hidden below `lg`)
- Back-to-list discards filter state
- Source field always disabled "manual" — remove from UI
- profileField i18n namespace duplicated (mutations.profileFields + profileFields)
- Detail page uses formatLabel(key) instead of i18n for profile fields

## UX Researcher (4C + 6P + 8D)

- Sheet→Edit Sheet nested depth disorienting
- Search combobox 2-char minimum shows "No results" without hint
- Overview doesn't surface topology problems
- Filter toolbar has 8 controls simultaneously (cognitive overload)
- Delighters: Cmd+K palette, posture bar, persistent create prefs, column visibility toggle

## UI Designer (6H + 5M)

- Font size fragmentation: `text-[10px]`, `text-[11px]` outside standard scale
- Role colors overlap with zone palette hues
- Topology colors not dark-mode adaptive (fixed Tailwind values)
- Monospace inconsistent for technical identifiers across contexts
- Problem panel always amber regardless of critical severity

## API Tester (4P1 + 4P2 + 4P3)

- auth handler leaks raw error on 500
- `archivedBy` spec type mismatch (string vs uint64)
- `profileSummary` field missing from OpenAPI Resource schema
- Dead code: topology_handler direction "" check unreachable
- No Content-Type: application/json validation on write endpoints
- PATCH empty body returns generic error, not ValidationError with fields
