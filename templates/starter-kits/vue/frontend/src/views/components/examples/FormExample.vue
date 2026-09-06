<script setup lang="ts">
import { useForm } from 'vee-validate'
import { toast } from 'vue-sonner'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { zodRule } from '@/lib/zod-rule'

const schema = z.object({
  name: z.string().min(3, { error: 'Project name must be at least 3 characters.' }),
  ownerEmail: z.email({ error: 'Enter a valid email address.' }),
})

const { handleSubmit } = useForm<z.infer<typeof schema>>({
  validationSchema: {
    name: zodRule(schema.shape.name),
    ownerEmail: zodRule(schema.shape.ownerEmail),
  },
  initialValues: { name: 'Acme Admin', ownerEmail: 'team@example.com' },
})

const submit = handleSubmit((values) => {
  toast.success(`Saved ${values.name}`, { description: `Owner: ${values.ownerEmail}` })
})
</script>

<template>
  <form class="grid gap-4" @submit.prevent="submit">
    <div class="grid gap-4 md:grid-cols-2">
      <FormField v-slot="{ componentField }" name="name">
        <FormItem>
          <FormLabel>Project name</FormLabel>
          <FormControl>
            <Input v-bind="componentField" placeholder="Acme Admin" />
          </FormControl>
          <FormDescription>Errors surface on the field that produced them.</FormDescription>
          <FormMessage />
        </FormItem>
      </FormField>

      <FormField v-slot="{ componentField }" name="ownerEmail">
        <FormItem>
          <FormLabel>Owner email</FormLabel>
          <FormControl>
            <Input v-bind="componentField" placeholder="team@example.com" />
          </FormControl>
          <FormDescription>Clear a field and submit to see validation.</FormDescription>
          <FormMessage />
        </FormItem>
      </FormField>
    </div>

    <Button type="submit" class="justify-self-start">Save settings</Button>
  </form>
</template>
