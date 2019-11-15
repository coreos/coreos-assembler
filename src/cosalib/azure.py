from cosalib.cmdlib import run_verbose


def remove_azure_image(image, resource_group, auth, profile):
    print(f"Azure: removing image {image}")
    try:
        run_verbose(['ore', 'azure',
                    '--azure-auth', auth,
                    '--azure-profile', profile,
                    'delete-image-arm',
                    '--image-name', image,
                    '--resource-group', resource_group])
    except SystemExit:
        raise Exception("Failed to remove image")
