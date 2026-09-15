package plugin

// OfficialRepo is one built-in plugin repository shipped with the server.
// The name comes from the repo.json "name" field and identifies the repo
// in the marketplace, the plugin_instances.source column and the repo
// cache directory.
type OfficialRepo struct {
	Name string
	URL  string
}

// OfficialRepos are the built-in plugin repositories. Add new official
// repos here; they are seeded into plugin_repos at startup (existing rows
// are never overwritten, so admins keep their custom edits).
var OfficialRepos = []OfficialRepo{
	{
		Name: "demo-plugins",
		URL:  "https://github.com/hyuan280/Sonicore-PluginSDK/raw/refs/heads/main/examples/repo.json",
	},
}
