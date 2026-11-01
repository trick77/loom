import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  getUserDirectives,
  getUserFacts,
  type UserDirective,
  type UserFact,
  type UserFactBucket,
} from "./api";
import { Icon } from "./chat/Icon";
import { formatTimeAgo } from "./timeago";

const BUCKET_LABELS: Record<UserFactBucket, string> = {
  long_term: "memory.bucketLongTerm",
  top_of_mind: "memory.bucketTopOfMind",
  recent: "memory.bucketRecent",
  earlier: "memory.bucketEarlier",
};

const CATEGORY_LABELS: Record<UserFact["category"], string> = {
  work: "memory.categoryWork",
  personal: "memory.categoryPersonal",
  interest: "memory.categoryInterest",
};

/** groupByBucket keeps the server's order: buckets and facts arrive sorted. */
function groupByBucket(
  facts: UserFact[],
): { bucket: UserFactBucket; facts: UserFact[] }[] {
  const groups: { bucket: UserFactBucket; facts: UserFact[] }[] = [];
  for (const fact of facts) {
    const last = groups[groups.length - 1];
    if (last && last.bucket === fact.bucket) last.facts.push(fact);
    else groups.push({ bucket: fact.bucket, facts: [fact] });
  }
  return groups;
}

/**
 * UserMemoryPanel shows the two layers of personal memory injected into every
 * thread, both read-only here:
 *
 * 1. Facts — learned in the background from the user's threads, grouped by
 *    how recently each came up (long-term first). Remembering, changing or
 *    forgetting one happens by telling Loom in chat; there are no edit or
 *    delete controls.
 * 2. Other instructions — the user's explicit standing instructions, likewise
 *    steered only through chat.
 */
export function UserMemoryPanel() {
  const { t } = useTranslation();
  const [facts, setFacts] = useState<UserFact[]>([]);
  const [loading, setLoading] = useState(true);
  const [directives, setDirectives] = useState<UserDirective[]>([]);

  useEffect(() => {
    let active = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- back to loading before the fetch
    setLoading(true);
    getUserFacts()
      .then((items) => {
        if (active) setFacts(items);
      })
      .catch(() => {
        if (active) setFacts([]);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    getUserDirectives()
      .then((items) => {
        if (active) setDirectives(items);
      })
      .catch(() => {
        if (active) setDirectives([]);
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="flex flex-col gap-4">
      <section
        aria-label={t("memory.memoriesHeading")}
        className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
      >
        <h2 className="flex items-center gap-1.5 text-sm font-medium text-[#ecece6]">
          <Icon name="memory" size="21px" className="text-[#d5d2c9]" />
          <span>{t("memory.memoriesHeading")}</span>
        </h2>

        <p className="mt-1.5 text-[13px] leading-5 text-[#8a887f]">
          {t("memory.memoriesDescription")}
        </p>

        {loading ? (
          <p className="mt-3 text-sm text-[#807d74]">{t("memory.loading")}</p>
        ) : facts.length > 0 ? (
          <div className="mt-3 flex flex-col gap-4" data-user-facts>
            {groupByBucket(facts).map((group) => (
              <div key={group.bucket}>
                <h3 className="text-[13px] font-medium uppercase tracking-wide text-[#a8a59c]">
                  {t(BUCKET_LABELS[group.bucket])}
                </h3>
                <ul className="mt-1.5 flex flex-col gap-1.5">
                  {group.facts.map((fact) => (
                    <li
                      key={fact.id}
                      className="flex gap-2 text-base leading-6 text-[#f3f0e8]"
                    >
                      <span aria-hidden className="select-none text-[#807d74]">
                        •
                      </span>
                      <span className="flex flex-col">
                        <span>{fact.content}</span>
                        <span className="text-xs leading-5 text-[#807d74]">
                          {t(CATEGORY_LABELS[fact.category])} ·{" "}
                          {t("memory.lastSeen", {
                            ago: formatTimeAgo(fact.lastSeen),
                          })}
                        </span>
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        ) : (
          <p className="mt-3 text-sm leading-5 text-[#807d74]">
            {t("memory.memoriesEmpty")}
          </p>
        )}
      </section>

      <section
        aria-label={t("memory.otherInstructions")}
        className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
      >
        <h2 className="flex items-center gap-1.5 text-sm font-medium text-[#ecece6]">
          <Icon name="memory" size="21px" className="text-[#d5d2c9]" />
          <span>{t("memory.otherInstructions")}</span>
        </h2>

        <p className="mt-1.5 text-[13px] leading-5 text-[#8a887f]">
          {t("memory.otherInstructionsDescription")}
        </p>

        {directives.length > 0 ? (
          <ul className="mt-3 flex flex-col gap-1.5" data-user-directives>
            {directives.map((directive) => (
              <li
                key={directive.id}
                className="flex gap-2 text-base leading-6 text-[#f3f0e8]"
              >
                <span aria-hidden className="select-none text-[#807d74]">
                  •
                </span>
                <span>{directive.content}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="mt-3 text-sm leading-5 text-[#807d74]">
            {t("memory.otherInstructionsEmpty")}
          </p>
        )}
      </section>
    </div>
  );
}
