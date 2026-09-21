import {
  ArchiveIcon,
  BroadcastIcon,
  CubeIcon,
  DatabaseIcon,
  FunctionIcon,
  GlobeIcon,
  KeyIcon,
  LightningIcon,
  QueueIcon,
  ShareNetworkIcon,
} from "phosphor-svelte";
import type { NodeKind } from "$lib/components/topology/types";

/** One glyph per resource kind, so a tile reads without its label. */
export const KIND_ICON: Record<NodeKind, typeof CubeIcon> = {
  gateway: GlobeIcon,
  eventbridge: LightningIcon,
  topic: BroadcastIcon,
  queue: QueueIcon,
  function: FunctionIcon,
  dynamodb: DatabaseIcon,
  bucket: ArchiveIcon,
  secret: KeyIcon,
  extension: ShareNetworkIcon,
  infra: CubeIcon,
};
