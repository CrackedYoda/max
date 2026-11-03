import { Header } from "@/components/Header";
import { ButtonLink } from "@/components/ui/Button";

export default function NotFound() {
  return (
    <div className="min-h-screen">
      <Header />
      <main className="mx-auto flex max-w-[640px] flex-col items-start gap-4 px-6 py-24">
        <h1 className="m-0 font-display text-[40px] font-medium">Page not found</h1>
        <p className="m-0 text-muted">That page doesn’t exist.</p>
        <ButtonLink href="/">Go home</ButtonLink>
      </main>
    </div>
  );
}
