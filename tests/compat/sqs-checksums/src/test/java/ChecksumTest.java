import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import java.net.URI;
import java.util.Map;
import java.util.UUID;
import software.amazon.awssdk.auth.credentials.*;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.core.SdkBytes;
import software.amazon.awssdk.services.sqs.SqsClient;
import software.amazon.awssdk.services.sqs.model.*;
import software.amazon.awssdk.services.sns.SnsClient;

class ChecksumTest {
    final String endpoint = System.getProperty("tarn.endpoint", "http://127.0.0.1:24566");
    final StaticCredentialsProvider credentials = StaticCredentialsProvider.create(AwsBasicCredentials.create("local", "local"));
    SqsClient sqs() { return SqsClient.builder().endpointOverride(URI.create(endpoint)).region(Region.EU_WEST_2).credentialsProvider(credentials).build(); }
    Map<String, MessageAttributeValue> attributes() {
        return Map.of("a", MessageAttributeValue.builder().dataType("String.custom").stringValue("café 🌍").build(),
            "z", MessageAttributeValue.builder().dataType("Number.int").stringValue("42").build(),
            "blob", MessageAttributeValue.builder().dataType("Binary.png").binaryValue(SdkBytes.fromByteArray(new byte[]{0,-1,-128,1})).build());
    }
    @Test void directAndBatchV2() {
        try (var sqs = sqs()) {
            String queue = sqs.createQueue(r -> r.queueName("checksum-" + UUID.randomUUID())).queueUrl();
            try {
                assertEquals("36353936af40491f5594f4d0ef7dafc4", sqs.sendMessage(r -> r.queueUrl(queue).messageBody("hello").messageAttributes(attributes())).md5OfMessageAttributes());
                var batch = sqs.sendMessageBatch(r -> r.queueUrl(queue).entries(
                    SendMessageBatchRequestEntry.builder().id("a").messageBody("hello").messageAttributes(attributes()).build(),
                    SendMessageBatchRequestEntry.builder().id("b").messageBody("plain").build()));
                assertEquals(2, batch.successful().size());
                var messages = sqs.receiveMessage(r -> r.queueUrl(queue).messageAttributeNames("All").maxNumberOfMessages(10)).messages();
                assertEquals(3, messages.size());
                for (var m : messages) { if (!m.messageAttributes().isEmpty()) assertEquals(attributes(), m.messageAttributes()); sqs.deleteMessage(r -> r.queueUrl(queue).receiptHandle(m.receiptHandle())); }
                assertTrue(sqs.receiveMessage(r -> r.queueUrl(queue)).messages().isEmpty());
            } finally { sqs.deleteQueue(r -> r.queueUrl(queue)); }
        }
    }
    @Test void queryV1() {
        var sqs = com.amazonaws.services.sqs.AmazonSQSClientBuilder.standard()
            .withEndpointConfiguration(new com.amazonaws.client.builder.AwsClientBuilder.EndpointConfiguration(endpoint, "eu-west-2"))
            .withCredentials(new com.amazonaws.auth.AWSStaticCredentialsProvider(new com.amazonaws.auth.BasicAWSCredentials("local", "local"))).build();
        String queue = sqs.createQueue("checksum-v1-" + UUID.randomUUID()).getQueueUrl();
        try {
            var attrs = Map.of("eventType", new com.amazonaws.services.sqs.model.MessageAttributeValue().withDataType("String").withStringValue("vacancy-expired"));
            assertEquals("edb64b283f223d4680cfb8608c8e72d7", sqs.sendMessage(new com.amazonaws.services.sqs.model.SendMessageRequest(queue,"hello").withMessageAttributes(attrs)).getMD5OfMessageAttributes());
            var messages = sqs.receiveMessage(new com.amazonaws.services.sqs.model.ReceiveMessageRequest(queue).withMessageAttributeNames("All")).getMessages();
            assertEquals(1,messages.size()); assertEquals(attrs,messages.get(0).getMessageAttributes());
            sqs.deleteMessage(queue,messages.get(0).getReceiptHandle());
        } finally { sqs.deleteQueue(queue); sqs.shutdown(); }
    }
    @Test void snsFanoutV2() {
        try (var sqs=sqs(); var sns=SnsClient.builder().endpointOverride(URI.create(endpoint)).region(Region.EU_WEST_2).credentialsProvider(credentials).build()) {
            for (boolean raw : new boolean[]{false,true}) {
                String queue=sqs.createQueue(r -> r.queueName("fanout-"+UUID.randomUUID())).queueUrl();
                String arn=sqs.getQueueAttributes(r -> r.queueUrl(queue).attributeNames(QueueAttributeName.QUEUE_ARN)).attributes().get(QueueAttributeName.QUEUE_ARN);
                String topic=sns.createTopic(r -> r.name("fanout-"+UUID.randomUUID())).topicArn();
                try {
                    sns.subscribe(r -> r.topicArn(topic).protocol("sqs").endpoint(arn).attributes(Map.of("RawMessageDelivery",String.valueOf(raw))));
                    sns.publish(r -> r.topicArn(topic).message("synthetic-event"));
                    var messages=sqs.receiveMessage(r -> r.queueUrl(queue).messageAttributeNames("All").waitTimeSeconds(2)).messages();
                    assertEquals(1,messages.size()); var message=messages.get(0);
                    assertTrue(message.messageAttributes().containsKey("correlationId"));
                    assertNotNull(message.md5OfMessageAttributes());
                    if(raw) assertEquals("synthetic-event",message.body()); else assertTrue(message.body().contains("Notification"));
                    sqs.deleteMessage(r -> r.queueUrl(queue).receiptHandle(message.receiptHandle()));
                    assertTrue(sqs.receiveMessage(r -> r.queueUrl(queue)).messages().isEmpty());
                    System.out.println("SNS -> SQS raw="+raw+" attributes="+message.messageAttributes().keySet()+" checksum="+message.md5OfMessageAttributes()+" acknowledged; queue drained");
                } finally {sns.deleteTopic(r -> r.topicArn(topic));sqs.deleteQueue(r -> r.queueUrl(queue));}
            }
        }
    }
    @Test void springTemplateAndListener() throws Exception {
        try (var sqs=sqs(); var async=software.amazon.awssdk.services.sqs.SqsAsyncClient.builder().endpointOverride(URI.create(endpoint)).region(Region.EU_WEST_2).credentialsProvider(credentials).build();
             var sns=SnsClient.builder().endpointOverride(URI.create(endpoint)).region(Region.EU_WEST_2).credentialsProvider(credentials).build()) {
            String queue=sqs.createQueue(r -> r.queueName("spring-"+UUID.randomUUID())).queueUrl();
            String arn=sqs.getQueueAttributes(r -> r.queueUrl(queue).attributeNames(QueueAttributeName.QUEUE_ARN)).attributes().get(QueueAttributeName.QUEUE_ARN);
            String topic=sns.createTopic(r -> r.name("spring-"+UUID.randomUUID())).topicArn();
            var received=new java.util.concurrent.LinkedBlockingQueue<String>();
            var container=io.awspring.cloud.sqs.listener.SqsMessageListenerContainer.<String>builder()
                .sqsAsyncClient(async).queueNames(queue).messageListener(message -> received.add(message.getPayload())).build();
            try {
                container.start();
                var template=io.awspring.cloud.sqs.operations.SqsTemplate.builder().sqsAsyncClient(async).build();
                template.send(to -> to.queue(queue).payload("template-event").header("eventType","vacancy-expired"));
                assertEquals("template-event",received.poll(10,java.util.concurrent.TimeUnit.SECONDS));
                for(boolean raw:new boolean[]{false,true}) {
                    var subscription=sns.subscribe(r -> r.topicArn(topic).protocol("sqs").endpoint(arn).attributes(Map.of("RawMessageDelivery",String.valueOf(raw)))).subscriptionArn();
                    sns.publish(r -> r.topicArn(topic).message("spring-fanout"));
                    String payload=received.poll(10,java.util.concurrent.TimeUnit.SECONDS);
                    assertNotNull(payload); assertTrue(payload.contains("spring-fanout"));
                    sns.unsubscribe(r -> r.subscriptionArn(subscription));
                    System.out.println("AWSpring listener raw="+raw+" business effect recorded; checksum validation enabled");
                }
                long deadline=System.nanoTime()+java.util.concurrent.TimeUnit.SECONDS.toNanos(10);
                do {
                    var counts=sqs.getQueueAttributes(r -> r.queueUrl(queue).attributeNames(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES,QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES_NOT_VISIBLE)).attributes();
                    if("0".equals(counts.get(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES)) && "0".equals(counts.get(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES_NOT_VISIBLE))) return;
                    Thread.sleep(50);
                } while(System.nanoTime()<deadline);
                fail("listener failed to acknowledge and drain queue");
            } finally {container.stop();sns.deleteTopic(r -> r.topicArn(topic));sqs.deleteQueue(r -> r.queueUrl(queue));}
        }
    }

}
