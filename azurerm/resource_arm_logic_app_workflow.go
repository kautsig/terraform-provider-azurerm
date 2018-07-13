package azurerm

import (
	"fmt"
	"log"

	"github.com/Azure/azure-sdk-for-go/services/logic/mgmt/2016-06-01/logic"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

var logicAppResourceName = "azurerm_logic_app"

// azurerm_logic_app_action_custom
// azurerm_logic_app_trigger_custom
// azurerm_logic_app_condition_custom?

func resourceArmLogicAppWorkflow() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmLogicAppWorkflowCreate,
		Read:   resourceArmLogicAppWorkflowRead,
		Update: resourceArmLogicAppWorkflowUpdate,
		Delete: resourceArmLogicAppWorkflowDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"location": locationSchema(),

			"resource_group_name": resourceGroupNameSchema(),

			// TODO: should Parameters be split out into their own object to allow validation on the different sub-types?
			"parameters": {
				Type:     schema.TypeMap,
				Optional: true,
			},

			"workflow_schema": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Default:  "https://schema.management.azure.com/providers/Microsoft.Logic/schemas/2016-06-01/workflowdefinition.json#",
			},

			"workflow_version": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Default:  "1.0.0.0",
			},

			"tags": tagsSchema(),

			"access_endpoint": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceArmLogicAppWorkflowCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).logicWorkflowsClient
	ctx := meta.(*ArmClient).StopContext

	log.Printf("[INFO] preparing arguments for Logic App Workflow creation.")

	name := d.Get("name").(string)
	resourceGroup := d.Get("resource_group_name").(string)
	location := azureRMNormalizeLocation(d.Get("location").(string))
	parameters := expandLogicAppWorkflowParameters(d)

	workflowSchema := d.Get("workflow_schema").(string)
	workflowVersion := d.Get("workflow_version").(string)
	tags := d.Get("tags").(map[string]interface{})

	properties := logic.Workflow{
		Location: utils.String(location),
		WorkflowProperties: &logic.WorkflowProperties{
			Definition: &map[string]interface{}{
				"$schema":        workflowSchema,
				"contentVersion": workflowVersion,
				"actions":        make(map[string]interface{}, 0),
				"triggers":       make(map[string]interface{}, 0),
			},
			Parameters: parameters,
		},
		Tags: expandTags(tags),
	}

	_, err := client.CreateOrUpdate(ctx, resourceGroup, name, properties)
	if err != nil {
		return fmt.Errorf("[ERROR] Error creating Logic App Workflow %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	read, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		return fmt.Errorf("[ERROR] Error making Read request on Logic App Workflow %q (Resource Group %q): %+v", name, resourceGroup, err)
	}
	if read.ID == nil {
		return fmt.Errorf("[ERROR] Cannot read Logic App Workflow %q (Resource Group %q) ID", name, resourceGroup)
	}

	d.SetId(*read.ID)

	return resourceArmLogicAppWorkflowRead(d, meta)
}

func resourceArmLogicAppWorkflowUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).logicWorkflowsClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := id.ResourceGroup
	name := id.Path["workflows"]

	// lock to prevent against Actions, Parameters or Triggers conflicting
	azureRMLockByName(name, logicAppResourceName)
	defer azureRMUnlockByName(name, logicAppResourceName)

	read, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			d.SetId("")
			return nil
		}

		return fmt.Errorf("[ERROR] Error making Read request on Logic App Workflow %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	if read.WorkflowProperties == nil {
		return fmt.Errorf("[ERROR] Error parsing Logic App Workflow - `WorkflowProperties` is nil")
	}

	location := azureRMNormalizeLocation(d.Get("location").(string))
	parameters := expandLogicAppWorkflowParameters(d)
	tags := d.Get("tags").(map[string]interface{})

	properties := logic.Workflow{
		Location: utils.String(location),
		WorkflowProperties: &logic.WorkflowProperties{
			Definition: read.WorkflowProperties.Definition,
			Parameters: parameters,
		},
		Tags: expandTags(tags),
	}

	_, err = client.CreateOrUpdate(ctx, resourceGroup, name, properties)
	if err != nil {
		return fmt.Errorf("Error updating Logic App Workspace %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	return resourceArmLogicAppWorkflowRead(d, meta)
}

func resourceArmLogicAppWorkflowRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).logicWorkflowsClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["workflows"]

	resp, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("[ERROR] Error making Read request on Logic App Workflow %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	d.Set("name", resp.Name)
	d.Set("resource_group_name", resourceGroup)

	if location := resp.Location; location != nil {
		d.Set("location", azureRMNormalizeLocation(*location))
	}

	if props := resp.WorkflowProperties; props != nil {
		parameters := flattenLogicAppWorkflowParameters(props.Parameters)
		if err := d.Set("parameters", parameters); err != nil {
			return fmt.Errorf("Error flattening `parameters`: %+v", err)
		}

		d.Set("access_endpoint", props.AccessEndpoint)

		if definition := props.Definition; definition != nil {
			if v, ok := definition.(map[string]interface{}); ok {
				schema := v["$schema"].(string)
				version := v["contentVersion"].(string)
				d.Set("workflow_schema", schema)
				d.Set("workflow_version", version)
			}
		}
	}

	flattenAndSetTags(d, resp.Tags)

	return nil
}

func resourceArmLogicAppWorkflowDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).logicWorkflowsClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["workflows"]

	// lock to prevent against Actions, Parameters or Triggers conflicting
	azureRMLockByName(name, logicAppResourceName)
	defer azureRMUnlockByName(name, logicAppResourceName)

	resp, err := client.Delete(ctx, resourceGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp) {
			return nil
		}

		return fmt.Errorf("Error issuing delete request for Logic App Workflow %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	return nil
}

func expandLogicAppWorkflowParameters(d *schema.ResourceData) map[string]*logic.WorkflowParameter {
	output := make(map[string]*logic.WorkflowParameter, 0)
	input := d.Get("parameters").(map[string]interface{})

	for k, v := range input {
		output[k] = &logic.WorkflowParameter{
			Type:  logic.ParameterTypeString,
			Value: v.(string),
		}
	}

	return output
}

func flattenLogicAppWorkflowParameters(input map[string]*logic.WorkflowParameter) map[string]interface{} {
	output := make(map[string]interface{}, 0)

	for k, v := range input {
		if v != nil {
			output[k] = v.Value.(string)
		}
	}

	return output
}
