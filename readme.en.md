# MyKeymap

A program helps you improve the efficiency of using the keyboard.


## Features

- Quickly start and switch any application
- ...

## Usage

- Enter `CapsLock`, `S`, `E` to open settings page.

## Screenshots
![settings](./doc/settings.en.png)

## GitHub configuration sync

The **GitHub configuration sync** card in Settings synchronizes `data/config.json` in both directions. On first use, enter the repository SSH URL and branch (default: `git@github.com:zAdventurer/MyKeymap.git`, `main`), then use **Push** to seed the remote repository with the current local configuration.

Sync uses the locally installed Git client and existing SSH credentials. MyKeymap never stores GitHub tokens, passwords, or private keys. Pull replaces the local file only when it has not changed since the last sync. If both copies changed, neither is overwritten: MyKeymap saves both backups, shows a redacted diff, and requires an explicit **Keep local** or **Use remote** choice.
