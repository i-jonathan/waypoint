import type { ExportFormat } from './types';

/** Where a download of the whole project in this format is. It is a plain link: the browser saves the file. */
export const exportHref = (project: string, format: ExportFormat): string => `/api/projects/${encodeURIComponent(project)}/export?format=${format}`;
