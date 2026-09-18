[![en](https://img.shields.io/badge/lang-en-red.svg)](https://github.com/xianyukang/MyKeymap/blob/master/readme.en.md)

# MyKeymap

MyKeymap 是一款基于 [AutoHotkey](https://www.autohotkey.com/) 的键盘映射工具，用于增强 Windows 的键盘输入体验和窗口操作效率。

## Features

- **程序启动切换**: 用快捷键启动程序和切换窗口，对比搜索型的启动器，效率更高
- **键盘控制鼠标**: 用键盘控制鼠标，能减少键鼠切换，不必为了点一下而大幅移动手掌
- **按键重新映射**: 
  - 把常用按键重映射到主键区，能大大提升输入速度、编辑文字的效率
  - 内置了几套键位负责:「 光标控制 」、「 数字输入 」、「 符号输入 」

## Usage

- [快速入门](https://xianyukang.com/MyKeymap.html#mykeymap-%E7%AE%80%E4%BB%8B) & [视频介绍](https://www.bilibili.com/video/BV1Sf4y1c7p8)
- [MyKeymap 2.0-beta33](https://wwqw.lanzouu.com/irujX2nesore) ( 提取码 1234 )

| ![features](./doc/features.png) | ![夏日大作战](./doc/夏日大作战.gif) |
| ------------------------------- | ----------------------------------- |

## Screenshots
![settings](./doc/settings.png)

## GitHub 配置同步

设置页的“GitHub 配置同步”可以将 `data/config.json` 与 GitHub 仓库双向同步。首次使用时填入仓库 SSH 地址和分支（默认 `git@github.com:zAdventurer/MyKeymap.git` 与 `main`），然后点击“推送”即可把当前本机配置创建为远端版本。

同步依赖本机安装的 Git 与已配置的 SSH 凭据；MyKeymap 不保存 GitHub Token、密码或私钥。拉取会在本机配置自上次同步后未变更时才替换文件。若本地和远端都已修改，程序不会覆盖任何一方：会保存双方备份、提供差异预览，并要求明确选择“保留本地”或“使用远端”。
