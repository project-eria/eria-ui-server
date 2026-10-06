import type { Property } from '../lib/client/eria';
import { getLocale } from '../lib/paraglide/runtime';
import { m } from '../lib/paraglide/messages';

const isoDate = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/;

/** Human text for a property value, in the current locale. */
export function formatValue(value: unknown, schema: Partial<Property> = {}, locale = getLocale()): string {
	const { text, unit } = formatParts(value, schema, locale);
	return unit ? `${text}${spaceBefore(unit)}${unit}` : text;
}

/** Number and unit apart, so the unit can be set smaller (large readings). */
export function formatParts(value: unknown, schema: Partial<Property> = {}, locale = getLocale()) {
	if (typeof value === 'number') {
		return { text: new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value), unit: schema.unit ?? '' };
	}
	return { text: formatText(value, locale), unit: '' };
}

function formatText(value: unknown, locale: ReturnType<typeof getLocale>): string {
	if (value === undefined || value === null) return m.no_value();
	if (typeof value === 'boolean') return value ? m.yes() : m.no();
	if (typeof value === 'string') {
		if (isoDate.test(value) && !Number.isNaN(Date.parse(value))) {
			return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
		}
		return value;
	}
	if (Array.isArray(value)) return new Intl.ListFormat(locale).format(value.map((v) => formatValue(v, {}, locale)));
	return JSON.stringify(value);
}

// SI style: narrow no-break space before units, except angle degrees (90°).
function spaceBefore(unit: string): string {
	return unit === '°' ? '' : ' ';
}
