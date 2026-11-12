import { useTranslation } from "react-i18next";

import { Icon } from "../chat/Icon";

/**
 * PassageCard is an idea's passage riding along with a message: outside text
 * (a page an answer quoted), shown apart from the user's own words, as plain
 * text. Removable in the composer, static in the sent message.
 */
export function PassageCard({
  text,
  source,
  onRemove,
}: {
  text: string;
  /** The title of the thread the passage came from. */
  source?: string;
  onRemove?: () => void;
}) {
  const { t } = useTranslation();
  const label =
    source !== undefined && source.trim() !== ""
      ? t("ideas.passageFrom", { source })
      : t("ideas.passage");
  return (
    <div
      className="relative w-[270px] max-w-full rounded-xl border border-[#45453f] bg-[#242422] px-2.5 pt-2 pb-2.5 text-left"
      data-testid="passage-card"
    >
      <div className="flex items-center gap-1.5 pr-6 text-[12px] text-unread-ink">
        <Icon name="bulb" size="14px" className="shrink-0" />
        <span className="truncate">{label}</span>
      </div>
      <p className="mt-1 line-clamp-3 whitespace-pre-wrap break-words font-serif text-[13px] font-[360] leading-[18px] text-[#a8a59c]">
        {text.slice(0, 600)}
      </p>
      {onRemove !== undefined && (
        <button
          type="button"
          aria-label={t("ideas.removePassage")}
          title={t("ideas.removePassage")}
          className="absolute top-1.5 right-1.5 grid h-5 w-5 place-items-center rounded-full bg-[#3a3a37] text-[#c7c5bd] hover:text-white"
          onClick={onRemove}
        >
          <Icon name="close" size="11px" />
        </button>
      )}
    </div>
  );
}
