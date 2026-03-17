export type NodeKind =
  | "gateway"
  | "topic"
  | "queue"
  | "bucket"
  | "function"
  | "secret"
  | "extension"
  | "infra";
export type ConnectionNode = {
  id: string;
  x: number;
  y: number;
  label: string;
  sub: string;
  kind: NodeKind;
  status?: string;
};
