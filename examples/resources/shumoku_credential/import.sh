# A credential is imported by its id (cr_...). Secret parameters cannot be read
# back: after the import, the next apply writes secret_params from configuration.
terraform import shumoku_credential.campus_snmp <credential_id>
