// paperplane-plugin-kotlin 是 kotlin 插件的独立可执行入口。
package main

import (
	sdk "github.com/lemon-paperplane/lemon-paperplane-main/lemon-paperplane-main-plugin-sdk"

	kotlin "github.com/lemon-paperplane/lemon-paperplane-plugin-kotlin/plugin"
)

func main() { sdk.Serve(kotlin.New()) }
