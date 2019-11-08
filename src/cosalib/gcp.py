from cosalib.cmdlib import run_verbose


def remove_gcp_image(gcp_id, json_key, project):
    print(f"GCP: removing image {gcp_id}")
    try:
        run_verbose(['ore', 'gcloud', 'delete-images', gcp_id,
                    '--json-key', json_key,
                    '--project', project])
    except SystemExit:
        raise Exception("Failed to remove image")
