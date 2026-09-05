/** Payload of GET /api/v1/version. Mirrors internal/version.Info. */
export interface BuildInfo {
  version: string;
  commit: string;
  sourceUrl: string;
}
