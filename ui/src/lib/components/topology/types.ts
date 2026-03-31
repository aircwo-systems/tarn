import type { BucketSummary } from "$lib/types";

export type NodeKind =
  | "gateway"
  | "eventbridge"
  | "topic"
  | "queue"
  | "dynamodb"
  | "bucket"
  | "function"
  | "secret"
  | "extension"
  | "infra";

/** Which edge of a node a connector port sits on. */
export type NodeSide = "top" | "bottom" | "left" | "right";

/**
 * Visual size of the node rectangle.
 * - small  — current rectangle (96×32 half-dims)
 * - medium — square with the same width (96×96 half-dims)
 * - large  — 2× the medium square (192×192 half-dims)
 */
export type NodeSize = "small" | "medium" | "large";
export type NodeView = string;

export type ConnectionNode = {
  id: string;
  x: number;
  y: number;
  label: string;
  sub: string;
  kind: NodeKind;
  status?: string;
  /** Which side the input (incoming wire) port is drawn on. Defaults to "left". */
  inputSide?: NodeSide;
  /** Which side the output (outgoing wire) port is drawn on. Defaults to "right". */
  outputSide?: NodeSide;
  /** Visual size of the node. Defaults to "small". */
  size?: NodeSize;
  /** Active renderer/view for the node. */
  view?: NodeView;
  /** Aggregate bucket preview data for bucket-specific views. */
  bucket?: BucketSummary;
};
