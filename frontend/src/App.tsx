export default function App() {
  return (
    <div className="grid h-screen grid-cols-[240px_1fr_300px] font-sans text-ink">
      <aside className="flex flex-col gap-2 bg-panel p-3 border-r border-border">
        <div className="font-serif text-xl font-semibold">spark</div>
        <button className="rounded-spark bg-accent px-3 py-2 text-sm text-white">
          + New chat
        </button>
      </aside>
      <main className="flex flex-col bg-bg p-6">
        <h1 className="font-serif text-lg">Welcome to spark</h1>
        <p className="text-muted">Foundation is up. Chat arrives in a later phase.</p>
      </main>
      <aside className="bg-panel border-l border-border p-3 text-sm text-muted">
        Context panel
      </aside>
    </div>
  );
}
