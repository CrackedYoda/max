"use client";

import type { MaxId } from "@max/sdk";
import { useEffect, useRef, useState } from "react";

import { errorMessage } from "@/error/messages";
import { useUpdateProfile } from "@/hooks/queries";
import { toast } from "@/store/flows";

import { Button } from "../ui/Button";
import { TextField } from "../ui/Fields";

import { Handle } from "./Handle";

export function EditProfileDialog({ id, onClose }: { id: MaxId; onClose(): void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [displayName, setDisplayName] = useState(id.displayName === id.name ? "" : id.displayName);
  const [description, setDescription] = useState(id.description);
  const update = useUpdateProfile();

  useEffect(() => {
    ref.current?.showModal();
  }, []);

  return (
    <dialog ref={ref} onClose={onClose} aria-labelledby="edit-title"
      className="m-auto w-[min(520px,calc(100vw-32px))] rounded-2xl border border-line bg-surface p-0 text-ink backdrop:bg-[rgba(20,20,19,0.4)]">
      <form method="dialog" className="flex flex-col gap-5 p-7"
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate({ id: id.id, displayName: displayName.trim(), description: description.trim() }, {
            onSuccess: () => {
              toast("Profile updated");
              ref.current?.close();
            },
          });
        }}>
        <div className="flex flex-col gap-1">
          <h2 id="edit-title" className="m-0 text-xl font-semibold">Edit profile</h2>
          <Handle name={id.name} className="text-base" />
        </div>
        <TextField id="edit-display" label="Display name" value={displayName} maxLength={64} onChange={(e) => setDisplayName(e.target.value)} placeholder="How your name appears on your profile" />
        <div className="flex flex-col gap-2.5">
          <label htmlFor="edit-desc" className="text-sm font-semibold">Description</label>
          <textarea id="edit-desc" rows={3} maxLength={280} value={description} onChange={(e) => setDescription(e.target.value)}
            className="resize-none rounded-xl border border-line bg-surface p-3.5 text-base outline-none focus:border-ink" />
          <span className="text-right text-xs text-muted">{description.length}/280</span>
        </div>
        {update.error ? <p role="alert" className="m-0 text-sm text-danger">{errorMessage(update.error)}</p> : null}
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => ref.current?.close()}>Cancel</Button>
          <Button type="submit" loading={update.isPending}>Save</Button>
        </div>
      </form>
    </dialog>
  );
}
