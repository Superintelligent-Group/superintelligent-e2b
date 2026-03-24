output "wake_function_url" {
  value       = aws_lambda_function_url.wake.function_url
  description = "URL to call to wake up the E2B cluster (used by swarm worker)"
}

output "wake_function_name" {
  value = aws_lambda_function.wake.function_name
}

output "shutdown_function_name" {
  value = aws_lambda_function.shutdown.function_name
}
