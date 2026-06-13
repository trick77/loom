export function ErrorText({ children }: { children: React.ReactNode }) {
  return (
    <div className="ui-meta-text mt-3 max-w-3xl rounded-lg border border-accent bg-[#282826] px-4 py-3 text-accent">
      {children}
    </div>
  );
}
