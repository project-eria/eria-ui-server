// Public SDK of ui-user for widget packs, served at the stable URL
// /sdk/eria-widgets.js (see PACKS.md). Packs import everything from here, so
// they share the app's behavior (following properties, commands, confirmation,
// errors) and only write their own rendering.
export * from 'lit';
export { EriaWidget, widgetStyles, type Variant, type WidgetErrorDetail } from '../widgets/base';
export { formatValue, formatParts } from '../widgets/format';
// Standard widgets, to extend when a pack changes only part of one.
export { EriaLight, EriaShutter, EriaGarage, EriaTrigger, EriaSensor, propertyName, compass } from '../widgets/capabilities';
export { EriaModes } from '../widgets/modes';
// The app's localized texts (FR/EN), so packs say "Allumée" like the rest of the UI.
export { m as messages } from '../lib/paraglide/messages';
export { getLocale } from '../lib/paraglide/runtime';

/** Contract version: a pack declares the one it was written for ("sdk" in pack.json). */
export const sdkVersion = 1;
