package main

// Themes (PLAN §5.3) are shipped with the UI: CSS values for the --eria-*
// variables, verified with each release. The backend only stores choices.
var themes = map[string]bool{"": true, "ardoise": true, "contraste": true} // "" = default / the profile's

// Color scheme: Auto follows the system, or force Light / Dark.
var schemes = map[string]bool{"": true, "auto": true, "light": true, "dark": true}
