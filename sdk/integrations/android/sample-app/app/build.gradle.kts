plugins {
    id("com.android.application")
}

fun declared(name: String, fallback: String): String =
    providers.gradleProperty(name).getOrElse(fallback)

val eventKeyId: String = declared("layerx.eventKeyId", "prod")

val declaredKeys: Map<String, String> = linkedMapOf(
    "layerx.service_url" to declared("layerx.serviceUrl", ""),
    "layerx.session_broker_url" to declared("layerx.sessionBrokerUrl", ""),
    "layerx.event_public_key.$eventKeyId" to declared("layerx.eventPublicKey", ""),
    "layerx.event_max_age_seconds" to declared("layerx.eventMaxAgeSeconds", "300"),
    "layerx.request_timeout_seconds" to declared("layerx.requestTimeoutSeconds", "30")
)

val secretScan: Configuration by configurations.creating

android {
    namespace = "com.sidiora.layerx.android.sample"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.sidiora.layerx.android.sample"
        minSdk = 34
        targetSdk = 34
        versionCode = 1
        versionName = "0.1.0"

        resValue("string", "layerx_service_url", declared("layerx.serviceUrl", ""))
        resValue("string", "layerx_session_broker_url", declared("layerx.sessionBrokerUrl", ""))
        resValue("string", "layerx_event_public_key", declared("layerx.eventPublicKey", ""))
        resValue("string", "layerx_event_max_age_seconds", declared("layerx.eventMaxAgeSeconds", "300"))
        resValue("string", "layerx_request_timeout_seconds", declared("layerx.requestTimeoutSeconds", "30"))
        resValue("string", "layerx_event_key_id", eventKeyId)
        resValue("string", "layerx_receipt_relay_url", declared("layerx.receiptRelayUrl", ""))
        resValue("string", "layerx_sample_asset", declared("layerx.sampleAsset", ""))
        resValue("string", "layerx_sample_recipient", declared("layerx.sampleRecipient", ""))
        resValue("string", "layerx_sample_amount", declared("layerx.sampleAmount", "0"))
        resValue("string", "layerx_sample_quote_json", declared("layerx.sampleQuoteJson", "{}"))
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }

    packaging {
        resources.excludes += setOf("META-INF/versions/9/OSGI-INF/MANIFEST.MF")
    }

    sourceSets {
        getByName("main") {
            java.srcDirs("src/main/java", "../../src/sample/java")
        }
    }
}

dependencies {
    implementation("com.sidiora.layerx:layerx-android:0.1.0")
    implementation("com.sidiora.layerx:layerx-sdk:0.1.0")
    implementation("com.fasterxml.jackson.core:jackson-databind:2.17.2")
    implementation("org.bouncycastle:bcprov-jdk18on:1.78.1")
    secretScan("com.sidiora.layerx:layerx-android:0.1.0")
}

androidComponents {
    onVariants { variant ->
        val capitalized = variant.name.replaceFirstChar { it.uppercase() }
        tasks.register<JavaExec>("layerxSecretScan$capitalized") {
            group = "verification"
            description = "Fails when the ${variant.name} artifact carries API secrets or key material"
            dependsOn("assemble$capitalized")
            classpath = secretScan
            mainClass.set("com.sidiora.layerx.android.EmbeddedSecretScan")
            doFirst {
                val declaredKeysFile = layout.buildDirectory
                    .file("layerx/${variant.name}-declared-keys.json").get().asFile
                declaredKeysFile.parentFile.mkdirs()
                declaredKeysFile.writeText(groovy.json.JsonOutput.toJson(declaredKeys))
                args = listOf(
                    layout.buildDirectory.dir("outputs/apk/${variant.name}").get().asFile.absolutePath,
                    declaredKeysFile.absolutePath
                )
            }
        }
    }
}
