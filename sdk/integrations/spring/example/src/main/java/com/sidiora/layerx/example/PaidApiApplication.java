package com.sidiora.layerx.example;

import com.fasterxml.jackson.databind.JsonNode;
import com.sidiora.layerx.spring.LayerXResource;
import com.sidiora.layerx.spring.LayerXResourceHandler;
import com.sidiora.layerx.spring.LayerXWebhookEventHandler;
import com.sidiora.layerx.spring.PlatformIntegration;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.context.annotation.Bean;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

@SpringBootApplication
@RestController
public class PaidApiApplication {
    private final List<Map<String, Object>> settlements = new ArrayList<>();

    public static void main(String[] arguments) {
        SpringApplication.run(PaidApiApplication.class, arguments);
    }

    @Bean
    public LayerXResourceHandler layerXResourceHandler(@Value("${example.resource-file:./resource.json}") String file) {
        return request -> new LayerXResource("application/json", Files.readAllBytes(Path.of(file)));
    }

    @Bean
    public LayerXWebhookEventHandler layerXWebhookEventHandler() {
        return (JsonNode event, String deliveryId) -> {
            Map<String, Object> record = new LinkedHashMap<>();
            record.put("deliveryId", deliveryId);
            record.put("event", event);
            synchronized (settlements) {
                settlements.add(record);
            }
        };
    }

    @GetMapping("/layerx/settlements")
    public Map<String, Object> settlements() {
        synchronized (settlements) {
            return Map.of("settlements", List.copyOf(settlements));
        }
    }

    @GetMapping("/layerx/integration")
    public Map<String, Object> integration() {
        return PlatformIntegration.platform_int_spring();
    }
}
