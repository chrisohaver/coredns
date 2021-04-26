package currentplugin

// Key is the context value representing the current plugin. Used by the RequestCount metric. When
// writing the metric, the value represents the "deepest" plugin reached in the chain, and it is assumed that
// this is the plugin responsible for "creating" the response written to the client.
type Key struct{}
