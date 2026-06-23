import { useEffect, useRef, useState } from "react";

import { ICONS, Icon, type IconName } from "./Icon";
import promptStarters from "./promptStarters.json";

type PromptCategory = {
  key: string;
  label: string;
  icon: IconName;
  suggestions: string[];
};

/** Validates a glyph name from the JSON against the icon font at load time, so a
 * typo'd `icon` value fails fast here instead of rendering an empty glyph later. */
function toIconName(value: string): IconName {
  if (!(value in ICONS)) {
    throw new Error(`promptStarters.json: unknown icon "${value}"`);
  }
  return value as IconName;
}

const TEMPLATE: string = promptStarters.template;
const SAMPLE_SIZE: number = promptStarters.sampleSize;
const CATEGORIES: PromptCategory[] = promptStarters.categories.map((category) => ({
  key: category.key,
  label: category.label,
  icon: toIconName(category.icon),
  suggestions: category.suggestions,
}));

/** Wraps a suggestion in the prompt template, lowercasing the first letter so it
 * reads naturally inside "Could you …?". */
function buildPrompt(suggestion: string): string {
  const phrase = suggestion.charAt(0).toLowerCase() + suggestion.slice(1);
  return TEMPLATE.replace("{suggestion}", phrase);
}

/** Returns up to `count` randomly chosen items (Fisher–Yates on a copy). */
function randomSample(items: string[], count: number): string[] {
  const pool = [...items];
  for (let i = pool.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [pool[i], pool[j]] = [pool[j], pool[i]];
  }
  return pool.slice(0, Math.min(count, pool.length));
}

export function PromptStarters({ onPick }: { onPick(prompt: string): void }) {
  const [open, setOpen] = useState<{ category: PromptCategory; suggestions: string[] } | null>(null);
  const triggerRefs = useRef(new Map<string, HTMLButtonElement>());
  const headerRef = useRef<HTMLButtonElement>(null);
  const lastOpenedKey = useRef<string | null>(null);

  function toggle(category: PromptCategory) {
    setOpen((current) =>
      current?.category.key === category.key
        ? null
        : { category, suggestions: randomSample(category.suggestions, SAMPLE_SIZE) },
    );
  }

  // The button row and the panel swap places in the DOM, so without this the
  // browser drops focus to <body> on open/close. Move focus with the disclosure:
  // to the panel header when it opens, back to the originating button when it
  // closes. Skips the initial mount (lastOpenedKey starts null) so it never
  // steals focus from the composer.
  useEffect(() => {
    if (open !== null) {
      lastOpenedKey.current = open.category.key;
      headerRef.current?.focus();
    } else if (lastOpenedKey.current !== null) {
      triggerRefs.current.get(lastOpenedKey.current)?.focus();
      lastOpenedKey.current = null;
    }
  }, [open]);

  if (open === null) {
    return (
      <ul aria-label="Prompt categories" className="mt-4 flex flex-wrap justify-center gap-2">
        {CATEGORIES.map((category, index) => (
          <li
            key={category.key}
            className="prompt-pop-in"
            style={{ animationDelay: `${index * 50}ms` }}
          >
            <button
              ref={(el) => {
                if (el) triggerRefs.current.set(category.key, el);
                else triggerRefs.current.delete(category.key);
              }}
              className="ui-control-text flex h-8 items-center gap-1.5 rounded-lg bg-[rgba(255,255,255,0.1)] px-3 font-normal text-white transition-colors hover:bg-[rgba(255,255,255,0.16)]"
              type="button"
              onClick={() => toggle(category)}
            >
              <Icon className="text-[#97958c]" name={category.icon} size="1.3rem" />
              {category.label}
            </button>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <div className="prompt-panel-in mx-auto mt-4 w-full max-w-[644px]">
      <div className="overflow-hidden rounded-2xl border border-[rgba(226,225,218,0.15)] bg-[#2c2c2a] p-2 shadow-[0_1px_2px_rgba(11,11,11,0.06),0_2px_8px_rgba(0,0,0,0.24)]">
        <button
          ref={headerRef}
          aria-label={`Close ${open.category.label} suggestions`}
          className="group ui-meta-text flex w-full items-center gap-2 px-2 py-1 text-left text-[#97958c]"
          type="button"
          onClick={() => toggle(open.category)}
        >
          <Icon name={open.category.icon} size="1rem" />
          {open.category.label}
          <span className="ml-auto transition-colors group-hover:text-[#f3f0e8]">
            <Icon name="close" size="0.95rem" />
          </span>
        </button>
        <ul aria-label={`${open.category.label} suggestions`} className="mt-1 flex flex-col">
          {open.suggestions.map((suggestion) => (
            <li key={suggestion} className="ui-prompt-option">
              <button
                className="ui-control-text flex w-full items-center rounded-lg px-2 py-2.5 text-left text-[#c3c2b7] transition-colors hover:bg-[rgba(255,255,255,0.05)] hover:text-[#f3f0e8]"
                type="button"
                onClick={() => {
                  onPick(buildPrompt(suggestion));
                  setOpen(null);
                }}
              >
                {suggestion}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
