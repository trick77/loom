export function AttachmentExtensionPill({ children }: { children: string }) {
  return (
    <span className="absolute bottom-1 left-1 rounded-[4px] border border-[#6a675f] bg-[#343432]/90 px-1.5 py-[1px] text-[10px] font-medium leading-[14px] text-[#d8d4ca] shadow-[0_1px_2px_rgba(0,0,0,0.22)]">
      {children}
    </span>
  );
}
