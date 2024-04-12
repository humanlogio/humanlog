# humanlog

Read logs from `stdin` and prints them back to `stdout`, but prettier.

# Using it

## On macOS

```bash
brew tap humanlogio/homebrew-tap
brew install humanlog
```

## On linux (and macOS)

```bash
curl -sSL "https://humanlog.io/install.sh" | sh
```

## Otherwise

[Grab a release](https://github.com/humanlogio/humanlog/releases)!

# Example

If you emit logs in JSON or in [`logfmt`](https://brandur.org/logfmt), you will enjoy pretty logs when those
entries are encountered by `humanlog`. Unrecognized lines are left unchanged.

```
$ humanlog < /var/log/logfile.log
```

![2__fish___users_antoine_gocode_src_github_com_humanlogio_humanlog__fish_](https://cloud.githubusercontent.com/assets/1189716/4328545/f2330bb4-3f86-11e4-8242-4f49f6ae9efc.png)

# Usage

```
NAME:
   humanlog - reads structured logs from stdin, makes them pretty on stdout!

USAGE:
   humanlog [global options] command [command options] [arguments...]

VERSION:
   0.7.2+48f11b1

AUTHOR:
   Antoine Grondin <antoinegrondin@gmail.com>

COMMANDS:
   version  Interact with humanlog versions
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --config value                    specify a config file to use, otherwise uses the default one
   --skip value                      keys to skip when parsing a log entry
   --keep value                      keys to keep when parsing a log entry
   --sort-longest                    sort by longest key after having sorted lexicographically
   --skip-unchanged                  skip keys that have the same value than the previous entry
   --truncate                        truncates values that are longer than --truncate-length
   --truncate-length value           truncate values that are longer than this length (default: 15)
   --color value                     specify color mode: auto, on/force, off (default: "auto")
   --light-bg                        use black as the base foreground color (for terminals with light backgrounds)
   --time-format value               output time format, see https://golang.org/pkg/time/ for details (default: "Jan _2 15:04:05")
   --ignore-interrupts, -i           ignore interrupts
   --message-fields value, -m value  Custom JSON fields to search for the log message. (i.e. mssge, data.body.message) [$HUMANLOG_MESSAGE_FIELDS]
   --time-fields value, -t value     Custom JSON fields to search for the log time. (i.e. logtime, data.body.datetime) [$HUMANLOG_TIME_FIELDS]
   --level-fields value, -l value    Custom JSON fields to search for the log level. (i.e. somelevel, data.level) [$HUMANLOG_LEVEL_FIELDS]
   --help, -h                        show help
   --version, -v                     print the version
```

[l2met]: https://github.com/ryandotsmith/l2met
