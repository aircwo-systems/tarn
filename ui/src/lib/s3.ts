export interface S3Object {
  key: string;
  size: number;
  lastModified: string;
  etag: string;
}

export interface S3Listing {
  prefixes: string[];
  objects: S3Object[];
  truncated: boolean;
}

export interface S3ObjectHead {
  contentType: string;
  contentLength: number;
  etag: string;
  lastModified: string;
}

export const MAX_TEXT_PREVIEW_BYTES = 256 * 1024;
export const MAX_IMAGE_PREVIEW_BYTES = 5 * 1024 * 1024;

export function objectPath(bucket: string, key = ""): string {
  const encodedKey = key.split("/").map(encodeURIComponent).join("/");
  return `/_s3/${encodeURIComponent(bucket)}${key ? `/${encodedKey}` : ""}`;
}

/** ListObjectsV2 scoped to one "folder" level: delimiter "/" splits keys into prefixes + objects. */
export async function listObjects(bucket: string, prefix: string): Promise<S3Listing> {
  const qs = new URLSearchParams({ "list-type": "2", delimiter: "/", "max-keys": "1000" });
  if (prefix) qs.set("prefix", prefix);
  const resp = await fetch(`${objectPath(bucket)}?${qs}`);
  if (!resp.ok) throw new Error(`List failed: HTTP ${resp.status}`);
  const xml = new DOMParser().parseFromString(await resp.text(), "text/xml");
  const text = (el: Element, tag: string) => el.getElementsByTagName(tag)[0]?.textContent ?? "";

  return {
    prefixes: Array.from(xml.getElementsByTagName("CommonPrefixes")).map((p) => text(p, "Prefix")),
    objects: Array.from(xml.getElementsByTagName("Contents"))
      .map((c) => ({
        key: text(c, "Key"),
        size: parseInt(text(c, "Size") || "0", 10),
        lastModified: text(c, "LastModified"),
        etag: text(c, "ETag").replaceAll('"', ""),
      }))
      // A zero-byte "folder/" placeholder is the prefix itself, not a child.
      .filter((o) => o.key !== prefix),
    truncated: xml.getElementsByTagName("IsTruncated")[0]?.textContent === "true",
  };
}

export async function headObject(bucket: string, key: string): Promise<S3ObjectHead> {
  const resp = await fetch(objectPath(bucket, key), { method: "HEAD" });
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
  return {
    contentType: resp.headers.get("content-type") ?? "application/octet-stream",
    contentLength: parseInt(resp.headers.get("content-length") ?? "0", 10) || 0,
    etag: (resp.headers.get("etag") ?? "").replaceAll('"', ""),
    lastModified: resp.headers.get("last-modified") ?? "",
  };
}

export async function putObject(bucket: string, key: string, file: File): Promise<void> {
  const resp = await fetch(objectPath(bucket, key), {
    method: "PUT",
    headers: { "content-type": file.type || "application/octet-stream" },
    body: file,
  });
  if (!resp.ok) throw new Error(`Upload failed: HTTP ${resp.status}`);
}

export async function deleteObject(bucket: string, key: string): Promise<void> {
  const resp = await fetch(objectPath(bucket, key), { method: "DELETE" });
  if (!resp.ok) throw new Error(`Delete failed: HTTP ${resp.status}`);
}

export function isImage(contentType: string): boolean {
  return contentType.startsWith("image/") && contentType !== "image/svg+xml";
}

export function isText(contentType: string): boolean {
  const t = contentType.toLowerCase();
  return (
    t.startsWith("text/") ||
    ["json", "xml", "javascript", "typescript", "yaml", "yml", "svg", "csv", "toml"].some((m) => t.includes(m))
  );
}

/** Last path segment of a key or prefix, keeping the trailing slash on folders. */
export function leafName(keyOrPrefix: string, parent: string): string {
  return keyOrPrefix.slice(parent.length) || keyOrPrefix;
}
