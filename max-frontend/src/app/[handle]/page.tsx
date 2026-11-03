import { Suspense } from "react";

import { Header } from "@/components/Header";
import { ButtonLink } from "@/components/ui/Button";

import { ProfileView } from "./ProfileView";

export default function ProfilePage() {
  return (
    <div className="min-h-screen">
      <Header variant="action" action={<ButtonLink href="/claim" variant="secondary" size="sm" className="!font-medium">Get your own handle</ButtonLink>} />
      <Suspense>
        <ProfileView />
      </Suspense>
    </div>
  );
}
