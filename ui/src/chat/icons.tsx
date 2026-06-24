export function CopyIcon({ className = "h-[1.33rem] w-[1.33rem]" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M8 8.5V6.8c0-1 .8-1.8 1.8-1.8h7.4c1 0 1.8.8 1.8 1.8v7.4c0 1-.8 1.8-1.8 1.8h-1.7"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M5.8 8h7.4c1 0 1.8.8 1.8 1.8v7.4c0 1-.8 1.8-1.8 1.8H5.8c-1 0-1.8-.8-1.8-1.8V9.8C4 8.8 4.8 8 5.8 8Z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function SpeakerIcon() {
  return (
    <svg className="h-[1.33rem] w-[1.33rem]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M4 9.5h3l4-3.3v11.6l-4-3.3H4z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <path d="M15 9.2a4 4 0 0 1 0 5.6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M17.6 6.6a7.5 7.5 0 0 1 0 10.8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

export function CheckIcon({ className = "h-[1.33rem] w-[1.33rem]" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="m5 12.5 4.2 4.2L19 7"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function DownloadIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 4.5v9M7.5 10 12 14.5 16.5 10"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M5 18.5h14"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function RetryIcon() {
  return (
    <svg className="h-[1.33rem] w-[1.33rem]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M18.5 9.2A6.5 6.5 0 1 0 19 12"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <path
        d="M18.5 5.5v3.7h-3.7"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function FileIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M7 4.5h6.2L18 9.3v8.9c0 1-.8 1.8-1.8 1.8H7.8c-1 0-1.8-.8-1.8-1.8V6.3c0-1 .8-1.8 1.8-1.8Z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path
        d="M13 4.8V9h4.2"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
    </svg>
  );
}
