import { Engine } from "@/types/proto-es/v1/common_pb";

interface EngineStyle {
  label: string;
  icon: string;
  /** Avatar background and text, plus the badge's colored background/text. */
  bgClass: string;
  textClass: string;
  badgeClass: string;
}

/**
 * Engine presentation shared by every surface that shows an engine (the
 * instance page and the metadata browser). Keep the Tailwind class strings
 * literal: a name assembled at runtime would not survive content scanning.
 */
const ENGINE_STYLES: Partial<Record<Engine, EngineStyle>> = {
  [Engine.MYSQL]: {
    label: "MySQL",
    icon: "My",
    bgClass: "bg-warning-subtle",
    textClass: "text-warning",
    badgeClass: "bg-warning-subtle text-warning",
  },
  [Engine.POSTGRES]: {
    label: "PostgreSQL",
    icon: "PG",
    bgClass: "bg-info-subtle",
    textClass: "text-info",
    badgeClass: "bg-info-subtle text-info",
  },
  [Engine.TIDB]: {
    label: "TiDB",
    icon: "Ti",
    bgClass: "bg-primary-subtle",
    textClass: "text-primary-subtle-foreground",
    badgeClass: "bg-primary-subtle text-primary-subtle-foreground",
  },
  [Engine.MARIADB]: {
    label: "MariaDB",
    icon: "Ma",
    bgClass: "bg-success-subtle",
    textClass: "text-success",
    badgeClass: "bg-success-subtle text-success",
  },
  [Engine.OCEANBASE]: {
    label: "OceanBase",
    icon: "OB",
    bgClass: "bg-info-subtle",
    textClass: "text-info",
    badgeClass: "bg-info-subtle text-info",
  },
  [Engine.STARROCKS]: {
    label: "StarRocks",
    icon: "SR",
    bgClass: "bg-primary-subtle",
    textClass: "text-primary-subtle-foreground",
    badgeClass: "bg-primary-subtle text-primary-subtle-foreground",
  },
  [Engine.DORIS]: {
    label: "Doris",
    icon: "Do",
    bgClass: "bg-success-subtle",
    textClass: "text-success",
    badgeClass: "bg-success-subtle text-success",
  },
  [Engine.MSSQL]: {
    label: "SQL Server",
    icon: "MS",
    bgClass: "bg-danger-subtle",
    textClass: "text-danger",
    badgeClass: "bg-danger-subtle text-danger",
  },
};

const FALLBACK_STYLE: EngineStyle = {
  label: "Unknown",
  icon: "DB",
  bgClass: "bg-muted",
  textClass: "text-muted-foreground",
  badgeClass: "bg-muted text-muted-foreground",
};

function styleOf(engine?: Engine): EngineStyle {
  return (engine == null ? undefined : ENGINE_STYLES[engine]) ?? FALLBACK_STYLE;
}

export function engineLabel(engine?: Engine): string {
  return styleOf(engine).label;
}

export function engineIcon(engine?: Engine): string {
  return styleOf(engine).icon;
}

export function engineBgClass(engine?: Engine): string {
  return styleOf(engine).bgClass;
}

export function engineTextClass(engine?: Engine): string {
  return styleOf(engine).textClass;
}

/** Extra classes for a `Badge variant="secondary"` so the pill is engine-colored. */
export function engineBadgeClass(engine?: Engine): string {
  return `px-2 py-1 text-xs font-medium rounded-full ${styleOf(engine).badgeClass}`;
}

/**
 * Engines with no real schema level: a table lives directly under its database.
 * MySQL's `schema` is a synonym for database, and the TiDB/StarRocks/Doris/
 * OceanBase family follows it, so asking the user to pick a schema would only
 * offer an empty level.
 */
const SCHEMA_LESS_ENGINES: ReadonlySet<Engine> = new Set([
  Engine.MYSQL,
  Engine.TIDB,
  Engine.MARIADB,
  Engine.OCEANBASE,
  Engine.STARROCKS,
  Engine.DORIS,
]);

export function isSchemaLessEngine(engine: Engine | undefined): boolean {
  return engine !== undefined && SCHEMA_LESS_ENGINES.has(engine);
}
