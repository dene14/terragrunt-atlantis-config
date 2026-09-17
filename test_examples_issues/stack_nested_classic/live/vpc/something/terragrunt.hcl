terraform {
  source = "tfr:///terraform-aws-modules/lambda/aws?version=6.2.0"
}

inputs = {
  function_name = "something"
}