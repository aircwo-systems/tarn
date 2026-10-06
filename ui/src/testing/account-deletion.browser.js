import { mount, tick, unmount } from "svelte";
import AccountDeleteConfirmation from "$lib/components/sections/account-delete-confirmation.svelte";
import SettingsSection from "$lib/components/sections/settings-section.svelte";
import { getAccountSettings } from "$lib/state.svelte";

// Run in a dashboard dev-server tab:
// await (await import('/src/testing/account-deletion.browser.js')).checkAccountDeletion()
// The integration checks intercept account requests; no account is deleted on the server.
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const assert = (condition, message) => {
  if (!condition) throw new Error(message);
};
const pointer = (target, type, options = {}) =>
  target.dispatchEvent(
    new PointerEvent(type, { bubbles: true, pointerId: 7, isPrimary: true, button: 0, ...options }),
  );
const key = (target, type, value, options = {}) =>
  target.dispatchEvent(
    new KeyboardEvent(type, { bubbles: true, cancelable: true, key: value, ...options }),
  );
const waitFor = async (predicate, message) => {
  const deadline = performance.now() + 3500;
  while (!predicate()) {
    if (performance.now() > deadline) throw new Error(message);
    await pause(20);
  }
};

async function withConfirmation(check, props = {}) {
  const host = document.createElement("div");
  document.body.append(host);
  let confirmations = 0;
  let cancellations = 0;
  const component = mount(AccountDeleteConfirmation, {
    target: host,
    props: {
      accountId: "999999999990",
      label: "Synthetic account",
      onconfirm: () => confirmations++,
      oncancel: () => cancellations++,
      ...props,
    },
  });
  await tick();
  const button = host.querySelector(".hold-button");
  try {
    await check({
      host,
      button,
      confirmations: () => confirmations,
      cancellations: () => cancellations,
    });
  } finally {
    await unmount(component);
    host.remove();
  }
}

export async function checkAccountDeletion() {
  const passed = [];
  await withConfirmation(async ({ button, confirmations }) => {
    assert(document.activeElement === button, "Confirmation did not receive keyboard focus");
    button.click();
    pointer(button, "pointerdown", { button: 2 });
    key(button, "keydown", "Enter", { repeat: true });
    await pause(2100);
    assert(confirmations() === 0, "Click, right click, or repeated key triggered deletion");
    pointer(button, "pointerdown");
    await pause(150);
    assert(button.classList.contains("holding"), "Pointer hold did not show progress");
    pointer(window, "pointerup");
    await tick();
    assert(!button.classList.contains("holding"), "Release did not reset the hold");
    assert(button.textContent.includes("2.0s"), "Release did not reset the countdown");
    await pause(2100);
    assert(confirmations() === 0, "A short hold deleted the account later");
  });
  passed.push("clicks and short holds cannot delete; release resets the full duration");

  for (const cancellation of [
    "pointerleave",
    "pointercancel",
    "move",
    "window blur",
    "button blur",
  ]) {
    await withConfirmation(async ({ button, confirmations }) => {
      pointer(button, "pointerdown", { pointerType: "touch" });
      await pause(70);
      if (cancellation === "move") {
        const rect = button.getBoundingClientRect();
        pointer(button, "pointermove", {
          clientX: rect.right + 10,
          clientY: rect.top,
          pointerType: "touch",
        });
      } else if (cancellation === "window blur") window.dispatchEvent(new Event("blur"));
      else if (cancellation === "button blur") button.blur();
      else pointer(button, cancellation);
      await tick();
      await waitFor(() => !button.classList.contains("holding"), `${cancellation} did not cancel`);
      assert(confirmations() === 0, `${cancellation} triggered deletion`);
    });
  }
  passed.push("moving away, pointer cancellation, and focus loss cancel");

  await withConfirmation(async ({ button, confirmations, cancellations }) => {
    const spaceOnCancel = new KeyboardEvent("keyup", { bubbles: true, cancelable: true, key: " " });
    window.dispatchEvent(spaceOnCancel);
    assert(!spaceOnCancel.defaultPrevented, "Confirmation blocked Space on other controls");
    key(button, "keydown", " ");
    await pause(100);
    key(window, "keyup", " ");
    await tick();
    assert(!button.classList.contains("holding"), "Early keyboard release did not reset");
    pointer(button, "pointerdown");
    await pause(100);
    key(window, "keydown", "Escape");
    await pause(2100);
    assert(confirmations() === 0 && cancellations() === 1, "Escape failed to cancel the hold");
  });
  passed.push("keyboard release and Escape cancel without deleting");

  for (const method of ["pointer", "Enter", " "]) {
    await withConfirmation(async ({ button, confirmations }) => {
      if (method === "pointer") pointer(button, "pointerdown", { pointerType: "touch" });
      else key(button, "keydown", method);
      // Returning to an already-visible preview must not interrupt the hold.
      document.dispatchEvent(new Event("visibilitychange"));
      await pause(1000);
      assert(confirmations() === 0, `${method} confirmed before two seconds`);
      if (method !== "pointer") key(button, "keydown", method, { repeat: true });
      await waitFor(() => confirmations() === 1, `${method} did not complete the hold`);
      await tick();
      assert(
        button.disabled && button.textContent.includes("Deleting"),
        "No busy state after confirmation",
      );
      button.click();
      pointer(button, "pointerdown");
      key(button, "keydown", "Enter");
      await pause(100);
      assert(confirmations() === 1, "A completed hold submitted more than once");
    });
  }
  passed.push("touch, Enter, and Space require two seconds and submit exactly once");

  await withConfirmation(
    async ({ button, confirmations }) => {
      pointer(button, "pointerdown");
      key(button, "keydown", "Enter");
      await pause(2100);
      assert(confirmations() === 0, "Disabled confirmation deleted an account");
    },
    { busy: true },
  );
  let afterUnmount = 0;
  await withConfirmation(async ({ button }) => pointer(button, "pointerdown"), {
    onconfirm: () => afterUnmount++,
  });
  await pause(2100);
  assert(afterUnmount === 0, "Unmount left a destructive hold running");
  passed.push("busy controls and unmounted confirmations cannot delete");

  await checkSettingsDeletion();
  passed.push(
    "active and archived accounts use the same hold; failures allow a fresh retry; Cancel restores focus",
  );
  return { passed };
}

export async function checkSettingsDeletion() {
  const ids = ["999999999991", "999999999992"];
  assert(
    !getAccountSettings().knownAccounts.some((account) => ids.includes(account.id)),
    "Synthetic IDs must be unused",
  );
  const originalFetch = window.fetch;
  const savedAccounts = localStorage.getItem("tarn-accounts");
  let accounts = ids.map((id, index) => ({
    id,
    default: false,
    loaded: true,
    archived: index === 1,
    resources: { lambda: 1 },
    resourceTotal: 1,
  }));
  const deletions = [];
  let fail = true;
  let pending;
  window.fetch = async (input, init) => {
    const path = new URL(String(input), location.origin).pathname;
    if (path.endsWith("/_tarn/admin/accounts")) return Response.json({ accounts });
    if (path.includes("/_tarn/admin/accounts/") && init?.method === "DELETE") {
      const id = path.split("/").at(-1);
      assert(ids.includes(id), "Test attempted to delete a real account");
      deletions.push(id);
      return new Promise((resolve) => {
        pending = () => {
          if (fail)
            resolve(Response.json({ message: "Synthetic deletion failed" }, { status: 503 }));
          else {
            accounts = accounts.filter((account) => account.id !== id);
            resolve(Response.json({ accounts }));
          }
        };
      });
    }
    return originalFetch(input, init);
  };
  const host = document.createElement("main");
  document.body.append(host);
  const component = mount(SettingsSection, { target: host });
  const open = async (id) => {
    const trigger = host.querySelector(`#delete-account-${id}`);
    trigger.click();
    await tick();
    return host.querySelector(`[aria-label="Hold to delete account ${id}"]`);
  };
  try {
    await waitFor(
      () => host.querySelector(`#delete-account-${ids[0]}`),
      "Synthetic accounts did not load",
    );
    await open(ids[0]);
    host.querySelector(".cancel").click();
    await tick();
    await waitFor(
      () => document.activeElement === host.querySelector(`#delete-account-${ids[0]}`),
      "Cancel did not restore focus",
    );
    const failedButton = await open(ids[0]);
    pointer(failedButton, "pointerdown");
    await waitFor(() => deletions.length === 1, "Active account did not submit after holding");
    await tick();
    assert(failedButton.disabled, "Delete stayed enabled during the request");
    assert(
      host.querySelector(`[aria-label="Archive account ${ids[0]}"]`).disabled,
      "Archive allowed a concurrent action",
    );
    pending();
    await waitFor(
      () => host.textContent.includes("Synthetic deletion failed"),
      "Delete error was not shown",
    );
    assert(host.querySelector(`#delete-account-${ids[0]}`), "Failed deletion removed the account");
    fail = false;
    const retryButton = await open(ids[0]);
    pointer(retryButton, "pointerdown");
    await pause(150);
    assert(deletions.length === 1, "Retry reused the completed hold");
    await waitFor(() => deletions.length === 2, "Retry did not submit");
    pending();
    await waitFor(
      () => !host.querySelector(`#delete-account-${ids[0]}`),
      "Deleted account remained visible",
    );
    host.querySelector(".archived-toggle").click();
    await tick();
    const archivedButton = await open(ids[1]);
    key(archivedButton, "keydown", "Enter");
    await waitFor(() => deletions.length === 3, "Archived account did not require a hold");
    pending();
    await waitFor(
      () => !host.querySelector(`#delete-account-${ids[1]}`),
      "Deleted archived account remained visible",
    );
    assert(deletions.join(",") === [ids[0], ids[0], ids[1]].join(","), "Wrong account was deleted");
  } finally {
    await unmount(component);
    host.remove();
    window.fetch = originalFetch;
    if (savedAccounts === null) localStorage.removeItem("tarn-accounts");
    else localStorage.setItem("tarn-accounts", savedAccounts);
  }
}
