# Console Preferences Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current language dropdown with a direct `中 / EN` switch, add a fixed-preset accent-color picker, and mirror language/theme/accent preferences in settings without changing backend contracts.

**Architecture:** Keep preferences entirely in the frontend. Language continues to use the existing locale cookie and router refresh flow, theme continues to use `next-themes`, and accent becomes a CSS-variable-driven client preference applied through a root `data-accent` attribute. The topbar remains the quick-access surface, while settings becomes the fallback preference surface.

**Tech Stack:** Next.js App Router, TypeScript, next-intl, next-themes, Tailwind CSS v4, shadcn v4/base-nova, Vitest, Testing Library

---

## File Structure Map

### Frontend repository: `/Users/fan/JsProjects/ControlHub`

- Modify: `/Users/fan/JsProjects/ControlHub/app/globals.css`
- Modify: `/Users/fan/JsProjects/ControlHub/app/(console)/settings/page.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/app-shell/topbar.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/providers/app-providers.tsx`
- Create: `/Users/fan/JsProjects/ControlHub/components/providers/accent-provider.tsx`
- Create: `/Users/fan/JsProjects/ControlHub/components/settings/accent-switcher.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/settings/language-switcher.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/settings/theme-toggle.tsx`
- Create: `/Users/fan/JsProjects/ControlHub/lib/preferences.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/zh-CN.json`
- Modify: `/Users/fan/JsProjects/ControlHub/tests/components/language-switcher.test.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/tests/components/theme-toggle.test.tsx`
- Create: `/Users/fan/JsProjects/ControlHub/tests/components/accent-switcher.test.tsx`

## Assumptions

- Existing locale strategy stays cookie-based with no locale-prefixed routes.
- Accent preference stays frontend-only for phase 1.
- The four accent presets are `blue`, `purple`, `emerald`, and `amber`.
- Semantic status colors are not tied to accent tokens.

### Task 1: Add a shared console-preferences foundation

**Files:**
- Create: `/Users/fan/JsProjects/ControlHub/lib/preferences.ts`
- Create: `/Users/fan/JsProjects/ControlHub/components/providers/accent-provider.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/providers/app-providers.tsx`
- Test: `/Users/fan/JsProjects/ControlHub/tests/components/accent-switcher.test.tsx`

- [x] **Step 1: Write the failing accent-switcher test**

```tsx
import { NextIntlClientProvider } from "next-intl";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { AccentProvider } from "@/components/providers/accent-provider";
import { AccentSwitcher } from "@/components/settings/accent-switcher";
import messages from "@/messages/en.json";

describe("AccentSwitcher", () => {
  it("applies a purple accent to the document root", async () => {
    const user = userEvent.setup();

    render(
      <NextIntlClientProvider locale="en" messages={messages}>
        <AccentProvider>
          <AccentSwitcher />
        </AccentProvider>
      </NextIntlClientProvider>,
    );

    await user.click(screen.getByRole("button", { name: /accent/i }));
    await user.click(screen.getByRole("button", { name: /purple/i }));

    expect(document.documentElement.dataset.accent).toBe("purple");
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `npx vitest run tests/components/accent-switcher.test.tsx`
Expected: FAIL with missing `AccentProvider` or `AccentSwitcher`.

- [x] **Step 3: Write the minimal preference model**

```ts
// /Users/fan/JsProjects/ControlHub/lib/preferences.ts
export const ACCENT_STORAGE_KEY = "controlhub.accent";
export const ACCENTS = ["blue", "purple", "emerald", "amber"] as const;

export type AccentName = (typeof ACCENTS)[number];

export function isAccentName(value: string): value is AccentName {
  return ACCENTS.includes(value as AccentName);
}

export function readStoredAccent(): AccentName {
  if (typeof window === "undefined") {
    return "blue";
  }

  const value = window.localStorage.getItem(ACCENT_STORAGE_KEY);
  return value && isAccentName(value) ? value : "blue";
}
```

```tsx
// /Users/fan/JsProjects/ControlHub/components/providers/accent-provider.tsx
"use client";

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import {
  ACCENT_STORAGE_KEY,
  readStoredAccent,
  type AccentName,
} from "@/lib/preferences";

type AccentContextValue = {
  accent: AccentName;
  setAccent: (accent: AccentName) => void;
};

const AccentContext = createContext<AccentContextValue | null>(null);

export function AccentProvider({ children }: { children: ReactNode }) {
  const [accent, setAccentState] = useState<AccentName>("blue");

  useEffect(() => {
    const next = readStoredAccent();
    document.documentElement.dataset.accent = next;
    setAccentState(next);
  }, []);

  function setAccent(next: AccentName) {
    document.documentElement.dataset.accent = next;
    window.localStorage.setItem(ACCENT_STORAGE_KEY, next);
    setAccentState(next);
  }

  const value = useMemo(() => ({ accent, setAccent }), [accent]);

  return (
    <AccentContext.Provider value={value}>{children}</AccentContext.Provider>
  );
}

export function useAccent() {
  const context = useContext(AccentContext);
  if (!context) {
    throw new Error("useAccent must be used inside AccentProvider");
  }
  return context;
}
```

```tsx
// /Users/fan/JsProjects/ControlHub/components/providers/app-providers.tsx
import { AccentProvider } from "@/components/providers/accent-provider";

// inside providers tree
<ThemeProvider attribute="class" defaultTheme="system" enableSystem storageKey="controlhub.theme">
  <AccentProvider>
    <TooltipProvider delay={300}>{children}</TooltipProvider>
  </AccentProvider>
</ThemeProvider>
```

- [x] **Step 4: Add the minimal accent-switcher to make the test pass**

```tsx
// /Users/fan/JsProjects/ControlHub/components/settings/accent-switcher.tsx
"use client";

import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import { ACCENTS } from "@/lib/preferences";
import { useAccent } from "@/components/providers/accent-provider";

export function AccentSwitcher() {
  const t = useTranslations("controls.accent");
  const { accent, setAccent } = useAccent();

  return (
    <div>
      <Button aria-label={t("label")} variant="outline" size="icon-sm">
        <span className="size-2.5 rounded-full bg-primary" />
      </Button>
      <div className="sr-only">{accent}</div>
      {ACCENTS.map((option) => (
        <button key={option} type="button" onClick={() => setAccent(option)}>
          {t(`options.${option}`)}
        </button>
      ))}
    </div>
  );
}
```

- [x] **Step 5: Run the test to verify it passes**

Run: `npx vitest run tests/components/accent-switcher.test.tsx`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add components/providers/accent-provider.tsx components/providers/app-providers.tsx components/settings/accent-switcher.tsx lib/preferences.ts tests/components/accent-switcher.test.tsx
git commit -m "feat: add accent preference foundation"
```

### Task 2: Replace the language dropdown and add compact topbar controls

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/components/settings/language-switcher.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/settings/theme-toggle.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/settings/accent-switcher.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/app-shell/topbar.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/tests/components/language-switcher.test.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/tests/components/theme-toggle.test.tsx`

- [x] **Step 1: Update the failing language-switcher test to expect segmented buttons**

```tsx
it("renders direct Chinese and English switch buttons", () => {
  render(
    <NextIntlClientProvider locale="en" messages={messages}>
      <LanguageSwitcher />
    </NextIntlClientProvider>,
  );

  expect(screen.getByRole("button", { name: "中" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "EN" })).toBeInTheDocument();
});
```

- [x] **Step 2: Run the language test to verify it fails**

Run: `npx vitest run tests/components/language-switcher.test.tsx`
Expected: FAIL because the component still renders a select.

- [x] **Step 3: Replace the select with a segmented switch**

```tsx
// /Users/fan/JsProjects/ControlHub/components/settings/language-switcher.tsx
"use client";

import { useRouter } from "next/navigation";
import { useLocale } from "next-intl";

import { Button } from "@/components/ui/button";
import {
  LOCALE_COOKIE_NAME,
  isAppLocale,
  type AppLocale,
} from "@/i18n/locales";
import { cn } from "@/lib/utils";

const LOCALE_MAX_AGE_SECONDS = 60 * 60 * 24 * 365;

export function LanguageSwitcher() {
  const locale = useLocale();
  const router = useRouter();
  const value = isAppLocale(locale) ? locale : "zh-CN";

  function setLocale(nextLocale: AppLocale) {
    document.cookie = `${LOCALE_COOKIE_NAME}=${nextLocale}; path=/; max-age=${LOCALE_MAX_AGE_SECONDS}; SameSite=Lax`;
    router.refresh();
  }

  return (
    <div className="inline-flex h-9 items-center rounded-lg border border-border bg-card p-1">
      <Button
        variant="ghost"
        size="sm"
        aria-pressed={value === "zh-CN"}
        className={cn("h-7 px-2.5 text-xs", value === "zh-CN" && "bg-accent text-accent-foreground")}
        onClick={() => setLocale("zh-CN")}
      >
        中
      </Button>
      <Button
        variant="ghost"
        size="sm"
        aria-pressed={value === "en"}
        className={cn("h-7 px-2.5 text-xs", value === "en" && "bg-accent text-accent-foreground")}
        onClick={() => setLocale("en")}
      >
        EN
      </Button>
    </div>
  );
}
```

- [x] **Step 4: Make theme and accent controls align with topbar density**

```tsx
// /Users/fan/JsProjects/ControlHub/components/app-shell/topbar.tsx
<LanguageSwitcher />
<ThemeToggle />
<AccentSwitcher />
```

```tsx
"use client";

import { Palette } from "lucide-react";
import { useTranslations } from "next-intl";

import { useAccent } from "@/components/providers/accent-provider";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { ACCENTS } from "@/lib/preferences";
import { cn } from "@/lib/utils";

const swatchClassNames = {
  blue: "bg-[oklch(0.53_0.12_241.34)]",
  purple: "bg-[oklch(0.56_0.16_307.2)]",
  emerald: "bg-[oklch(0.62_0.12_161.4)]",
  amber: "bg-[oklch(0.72_0.14_74.2)]",
} as const;

export function AccentSwitcher() {
  const t = useTranslations("controls.accent");
  const { accent, setAccent } = useAccent();

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="icon-sm"
          aria-label={`${t("label")}: ${t(`options.${accent}`)}`}
          title={`${t("label")}: ${t(`options.${accent}`)}`}
        >
          <Palette className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-44 p-2">
        <div className="grid grid-cols-2 gap-2">
          {ACCENTS.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setAccent(option)}
              className={cn(
                "flex items-center gap-2 rounded-md border border-border px-2 py-2 text-left text-xs",
                accent === option && "border-primary bg-accent",
              )}
            >
              <span
                className={cn(
                  "size-3 rounded-full border border-black/10",
                  swatchClassNames[option],
                )}
              />
              {t(`options.${option}`)}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
```

- [x] **Step 5: Run focused tests**

Run: `npx vitest run tests/components/language-switcher.test.tsx tests/components/theme-toggle.test.tsx tests/components/accent-switcher.test.tsx`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add components/app-shell/topbar.tsx components/settings/language-switcher.tsx components/settings/theme-toggle.tsx components/settings/accent-switcher.tsx tests/components/language-switcher.test.tsx tests/components/theme-toggle.test.tsx tests/components/accent-switcher.test.tsx
git commit -m "feat: add compact topbar preference controls"
```

### Task 3: Add accent token maps and appearance settings fallback

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/app/globals.css`
- Modify: `/Users/fan/JsProjects/ControlHub/app/(console)/settings/page.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/zh-CN.json`

- [x] **Step 1: Add failing manual expectation notes for accent tokens**

```text
Expectation:
- changing data-accent updates --primary, --ring, --sidebar-primary
- success/warning/error colors remain unchanged
- purple remains legible in both light and dark
```

- [x] **Step 2: Add per-accent token maps in globals.css**

```css
/* /Users/fan/JsProjects/ControlHub/app/globals.css */
:root[data-accent="blue"] {
  --primary: oklch(0.53 0.12 241.34);
  --ring: oklch(0.53 0.12 241.34);
  --sidebar-primary: oklch(0.53 0.12 241.34);
}

:root[data-accent="purple"] {
  --primary: oklch(0.56 0.16 307.2);
  --ring: oklch(0.56 0.16 307.2);
  --sidebar-primary: oklch(0.56 0.16 307.2);
}

:root[data-accent="emerald"] {
  --primary: oklch(0.62 0.12 161.4);
  --ring: oklch(0.62 0.12 161.4);
  --sidebar-primary: oklch(0.62 0.12 161.4);
}

:root[data-accent="amber"] {
  --primary: oklch(0.72 0.14 74.2);
  --ring: oklch(0.72 0.14 74.2);
  --sidebar-primary: oklch(0.72 0.14 74.2);
}

.dark:root[data-accent="purple"],
.dark[data-accent="purple"] {
  --primary: oklch(0.72 0.12 307.2);
  --ring: oklch(0.72 0.12 307.2);
  --sidebar-primary: oklch(0.72 0.12 307.2);
}
```

- [x] **Step 3: Add an appearance section to settings**

```tsx
// /Users/fan/JsProjects/ControlHub/app/(console)/settings/page.tsx
import { AccentSwitcher } from "@/components/settings/accent-switcher";
import { LanguageSwitcher } from "@/components/settings/language-switcher";
import { ThemeToggle } from "@/components/settings/theme-toggle";

<DetailPanel
  title={t("pages.settings.appearance.title")}
  description={t("pages.settings.appearance.description")}
>
  <div className="flex flex-wrap items-center gap-3">
    <LanguageSwitcher />
    <ThemeToggle />
    <AccentSwitcher />
  </div>
</DetailPanel>
```

- [x] **Step 4: Add i18n copy for accent labels**

```json
{
  "controls": {
    "accent": {
      "label": "Accent",
      "options": {
        "blue": "Blue",
        "purple": "Purple",
        "emerald": "Emerald",
        "amber": "Amber"
      }
    }
  },
  "pages": {
    "settings": {
      "appearance": {
        "title": "Appearance",
        "description": "Language, theme, and primary accent preferences."
      }
    }
  }
}
```

- [x] **Step 5: Run build and lint**

Run:

```bash
npm run build
npm run lint
```

Expected:

- `build` PASS
- `lint` 0 errors, with only the existing TanStack warnings allowed

- [x] **Step 6: Commit**

```bash
git add app/globals.css app/(console)/settings/page.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add appearance preferences and accent tokens"
```

### Task 4: Final verification and manual acceptance

**Files:**
- No new files required

- [x] **Step 1: Run the full frontend verification suite**

Run:

```bash
npx vitest run
npm run build
npm run lint
```

Expected:

- tests PASS
- build PASS
- lint has no errors

- [x] **Step 2: Run manual browser verification**

Verify:

```text
http://localhost:3000/overview
http://localhost:3000/resources
http://localhost:3000/resources/40000000-0000-0000-0000-000000000002
http://localhost:3000/audits
http://localhost:3000/settings
```

Check:

- `中 / EN` changes the app language
- theme button cycles `light / dark / system`
- accent popover applies each preset
- purple remains readable in light and dark
- health and audit semantic colors do not change with accent

- [x] **Step 3: Commit the verification pass**

```bash
git add -A
git commit -m "test: verify console preference controls"
```

## Self-Review

- Spec coverage:
  - language segmented control: covered in Task 2
  - accent popover with 4 presets: covered in Tasks 1-3
  - appearance section in settings: covered in Task 3
  - semantic-color guardrail: covered in Task 3 token rules and Task 4 manual checks
- Placeholder scan:
  - no `TBD`, `TODO`, or implicit “handle later” language remains
- Type consistency:
  - accent values stay `blue | purple | emerald | amber`
  - language values stay `zh-CN | en`
