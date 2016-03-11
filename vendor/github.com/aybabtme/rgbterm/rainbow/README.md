# rainbooooowwww

An `io.Writer` for things that end up being printed to a terminal.

# Usage

Going back to work on Monday is hard. Cheerup your coworkers by adding
some colors to the production logs!

```go
joyfulOutput := rainbow.New(os.Stderr, 252, 255, 43)
log.SetOutput(joyfulOutput)
```

Wee, much happier coworkers!

![rainbow-demo](https://cloud.githubusercontent.com/assets/1189716/3563716/3dbd46ea-0a4d-11e4-8c6c-948d9925d0dd.gif)

# Caveats

Using that for your logs might not make your coworkers very happy.
