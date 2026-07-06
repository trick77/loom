import { useTranslation } from "react-i18next";

import { Icon } from "./Icon";

// PastedTextCard is the "Pasted" chip shown for a large collapsed paste — a
// 120×120 square card echoing claude.ai: a tiny clamped preview of the raw text
// above a plain uppercase "Pasted" badge. Shared by the composer (removable, with
// a close button) and the sent message bubble (static), so a paste looks identical
// while composing and after it is sent.
export function PastedTextCard({
  text,
  lineCount,
  onRemove,
}: {
  text: string;
  lineCount: number;
  onRemove?: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div
      className="group/attachment relative flex h-[120px] w-[120px] min-w-[120px] max-w-full flex-col justify-between gap-2.5 overflow-hidden rounded-lg border-[0.5px] border-[rgba(226,225,218,0.25)] bg-[#2c2c2a] px-2.5 py-2 text-[#f3f0e8] shadow-[0_1px_2px_rgba(11,11,11,0.06),0_2px_8px_rgba(0,0,0,0.24)] transition-colors hover:border-[rgba(226,225,218,0.4)]"
      title={t("composer.pastedText")}
    >
      {/* Preview of the raw text: tiny sans, wrapped, clamped to a few lines. Cap
          the substring so a multi-megabyte paste never renders in full. When the
          close button is shown (composer), indent the first line so the button does
          not overlap the preview text. */}
      <p
        className={`min-h-0 flex-1 overflow-hidden whitespace-pre-wrap break-all text-[8px] leading-[11px] text-[#aaa79e] [-webkit-box-orient:vertical] [-webkit-line-clamp:8] [display:-webkit-box] ${onRemove !== undefined ? "[text-indent:1.4rem]" : ""}`}
      >
        {text.slice(0, 400)}
      </p>
      <span className="flex-none text-[10px] font-semibold uppercase leading-none tracking-wide text-[#c3c2b7]">
        {t("composer.pastedBadge")}
      </span>
      {onRemove !== undefined && (
        <button
          className="absolute left-1 top-1 grid h-5 w-5 place-items-center rounded-full border border-[#64615a] bg-[#343432] text-[#d8d4ca] opacity-95 transition-colors hover:bg-[#44423d] hover:text-[#f3f0e8]"
          type="button"
          aria-label={t("composer.removePastedText", { count: lineCount })}
          onClick={onRemove}
        >
          <Icon name="closeCircle" size="14px" />
        </button>
      )}
    </div>
  );
}
