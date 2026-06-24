// ComingSoon is the neutral placeholder shown where the dev switcher is disabled
// (production). It is intentionally inert — no sign-in, nothing that calls the API —
// so a production build exposes neither the dev tool nor a way to create a stray
// auth identity. The real product UI replaces it in a later phase.
export function ComingSoon() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-slate-900 text-slate-100">
      <h1 className="font-mono text-2xl font-semibold text-teal-400">QLab</h1>
      <p className="mt-2 text-slate-400">Coming soon.</p>
    </div>
  );
}
