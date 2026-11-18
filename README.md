# lemon-paperplane-plugin-kotlin

Lemon Paperplane 的 Kotlin 插件（依赖 java，可选 gradle / maven）。

- 在 Gradle / Maven 工程里识别 Kotlin（Kotlin 插件、kotlin-stdlib、`src/main/kotlin`）。
- 主类：顶层 `fun main` 编译成文件门面类 `<文件名>Kt`（`@file:JvmName` 改名时以注解为准），也认 `object` / 伴生对象里的 `@JvmStatic main`；Spring Boot 启动类交给 spring-boot 插件。
- 运行配置 `kotlin-application`：构建插件的 `prepareClasspath` 编译取类路径，java 插件的 `buildCommand` 生成命令。导入 IDEA 的 Kotlin 运行配置。
- 诊断：Java/Kotlin JVM 目标不一致、Kotlin 不认识新 JVM 目标、Kotlin 元数据版本不兼容、编译守护进程异常。
