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
    bgClass: "bg-orange-100",
    textClass: "text-orange-600",
    badgeClass: "bg-orange-100 text-orange-700",
  },
  [Engine.POSTGRES]: {
    label: "PostgreSQL",
    icon: "PG",
    bgClass: "bg-blue-100",
    textClass: "text-blue-600",
    badgeClass: "bg-blue-100 text-blue-700",
  },
  [Engine.TIDB]: {
    label: "TiDB",
    icon: "Ti",
    bgClass: "bg-purple-100",
    textClass: "text-purple-600",
    badgeClass: "bg-purple-100 text-purple-700",
  },
  [Engine.MARIADB]: {
    label: "MariaDB",
    icon: "Ma",
    bgClass: "bg-teal-100",
    textClass: "text-teal-600",
    badgeClass: "bg-teal-100 text-teal-700",
  },
  [Engine.OCEANBASE]: {
    label: "OceanBase",
    icon: "OB",
    bgClass: "bg-cyan-100",
    textClass: "text-cyan-600",
    badgeClass: "bg-cyan-100 text-cyan-700",
  },
  [Engine.STARROCKS]: {
    label: "StarRocks",
    icon: "SR",
    bgClass: "bg-indigo-100",
    textClass: "text-indigo-600",
    badgeClass: "bg-indigo-100 text-indigo-700",
  },
  [Engine.DORIS]: {
    label: "Doris",
    icon: "Do",
    bgClass: "bg-lime-100",
    textClass: "text-lime-600",
    badgeClass: "bg-lime-100 text-lime-700",
  },
  [Engine.MSSQL]: {
    label: "SQL Server",
    icon: "MS",
    bgClass: "bg-red-100",
    textClass: "text-red-600",
    badgeClass: "bg-red-100 text-red-700",
  },
};

const FALLBACK_STYLE: EngineStyle = {
  label: "Unknown",
  icon: "DB",
  bgClass: "bg-gray-100",
  textClass: "text-gray-600",
  badgeClass: "bg-gray-100 text-gray-700",
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
