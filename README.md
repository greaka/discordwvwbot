# Discord Gw2 WvW Bot
A Discord Bot to automatically assign roles based on the gw2 wvw server.

You can find the official one under [wvwbot.tk](https://wvwbot.tk).

## Building it
This is written in Go 1.10.

```go get github.com/greaka/discordwvwbot```

I am happy about every contribution! Be it an issue or a PR.

## Contributing
If you want to contribute and introduce any breaking changes, then add some code to `migration.go` to have it not break on older versions.

## Discord Support Server
https://discord.gg/7dssenc

## How to run it

1. get the source
2. [build it](#building-it) (it is cross platform)
3. get a redis server
4. add a config.json based on the config.sample.json
5. execute it!
