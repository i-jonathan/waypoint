// The shapes of the Waypoint API. Field names are the Go JSON tags, which are
// the source of truth: internal/app/view_json.go, internal/domain/board.go,
// internal/config/appearance.go and internal/http. tests/contract.test.ts
// checks these types against real server output.

/** The body of every error response. */
export interface ApiErrorBody {
  error: string;
  code: string;
  /** The line of the board file a `parse` error points at. */
  line?: number;
  /** A `parse` error's text alone, with no path or line prefix. */
  message?: string;
  /** On a 409: the card, or the board for a settings change. */
  current?: CardView | BoardView;
}

/** An extra card field that the board file keeps verbatim, in its original order. */
export interface ExtraField {
  key: string;
  value: string;
}

/** A card as stored. */
export interface Card {
  id: string;
  title: string;
  column: string;
  /** A group name, or '' for none. */
  group: string;
  /** A priority name, or '' for none. */
  priority: string;
  depends: string[];
  /**
   * A UTC timestamp (2026-11-04T23:52:10Z), a date-only value (2026-10-26) or
   * null. Show it with displayDate.
   */
  created: string | null;
  /** Same shape as created. */
  done: string | null;
  body: string;
  extra: ExtraField[];
}

/** A dependency that is not yet done. `missing` means no card has that id. */
export interface Blocker {
  id: string;
  missing: boolean;
}

export interface Checklist {
  done: number;
  total: number;
}

/** A card with the facts the server derives from its board. */
export interface CardView extends Card {
  archived: boolean;
  /** Empty for an archived card. */
  blockers: Blocker[];
  /** True only for the one card that is next up. */
  next: boolean;
  checklist: Checklist;
  /** Send back as `if_match` to refuse a change made against a stale card. */
  hash: string;
}

export interface Column {
  name: string;
  /** 0 means no limit. */
  wip: number;
  /** The columns a card may move to from this one. */
  moves_to: string[];
  /** True for a done column. */
  done: boolean;
}

export interface Group {
  name: string;
  color: string;
}

export type ArchiveMode = 'off' | 'manual' | 'auto';

export interface ArchiveSettings {
  mode: ArchiveMode;
  after_days: number;
}

export type Theme = 'system' | 'light' | 'dark';
export type Density = 'comfortable' | 'compact';
export type Swimlane = 'none' | 'group' | 'priority';
export type Textures = 'on' | 'off';

/** Display overrides. An empty string inherits the next level. */
export interface Appearance {
  theme: Theme | '';
  /** A preset name or #rrggbb. */
  accent: string;
  density: Density | '';
  swimlane: Swimlane | '';
  textures: Textures | '';
}

export type AppearanceSource = 'project' | 'global' | 'default';

/** The appearance a board is shown in, after overrides and defaults are merged. */
export interface ResolvedAppearance {
  theme: Theme;
  accent: string;
  density: Density;
  swimlane: Swimlane;
  textures: Textures;
  /** Which level supplied each field, keyed by field name. */
  source: Record<string, AppearanceSource>;
}

/** A board's settings. */
export interface Settings {
  name: string;
  prefix: string;
  next_id: number;
  columns: Column[];
  groups: Group[];
  priorities: string[];
  archive: ArchiveSettings;
  appearance: Appearance;
}

export interface BoardView {
  settings: Settings;
  cards: CardView[];
  warnings: string[];
  /** Send back as `if_match` when saving the settings. */
  settings_hash: string;
}

/** What the board and project settings routes answer. */
export interface BoardResponse {
  board: BoardView;
  appearance: ResolvedAppearance;
  /** Always false: a board with a parse error is answered with 423. */
  readonly: boolean;
}

export interface ProjectInfo {
  name: string;
  storage: string;
  path: string;
  /** The accent the project resolves to. */
  accent: string;
  /** Set when the project cannot be opened, and says why. */
  error?: string;
}

export type TlsMode = 'auto' | 'on' | 'off';

/** The global config as the settings screen shows it. */
export interface GlobalSettings {
  port: number;
  open_browser: boolean;
  tls: TlsMode;
  appearance: Appearance;
  /** True when a new port or tls mode needs a server restart. */
  restart_required: boolean;
}

/** The body of PUT /api/settings. A field left out is kept. */
export interface GlobalSettingsChange {
  port?: number;
  open_browser?: boolean;
  tls?: TlsMode;
  /** A field left out is kept; an empty one goes back to inheriting. */
  appearance?: Partial<Appearance>;
}

export interface NewProject {
  name: string;
  /** Absolute, ending in .md or .markdown. Omit to let the server choose. */
  path?: string;
  prefix?: string;
}

export interface NewCard {
  title: string;
  column?: string;
  group?: string;
  priority?: string;
  body?: string;
  depends?: string[];
  extra?: ExtraField[];
}

/** The body of PATCH .../cards/{id}. A field left out is kept. */
export interface CardPatch {
  title?: string;
  /** '' clears it. */
  group?: string;
  /** '' clears it. */
  priority?: string;
  depends?: string[];
  body?: string;
  /** Extra fields to set; an empty value deletes the key. */
  set?: Record<string, string>;
}

/** The body of PUT .../settings. */
export interface ProjectSettingsChange {
  settings: Settings;
  /** The settings_hash of the settings that were edited. Required. */
  if_match: string;
  /** Old name to new name, for columns, groups and priorities renamed in the edit. */
  column_renames?: Record<string, string>;
  group_renames?: Record<string, string>;
  priority_renames?: Record<string, string>;
}
