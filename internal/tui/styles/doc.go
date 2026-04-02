// Package styles provides the shared Lip Gloss theme and colour palette for Hive.
//
// All colours, borders, and text styles used by TUI components are defined here.
// Components must import and use styles from this package rather than hardcoding
// ANSI codes or raw colour values.
//
// The exported [Theme] variable holds the active theme. Future work may support
// multiple themes; components should always reference Theme fields rather than
// the style constructors directly.
package styles
